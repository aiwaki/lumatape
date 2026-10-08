//! Bounded, authenticated Tauri/NSIS update channel. No network without release configuration.
//! The policy follows Slipstream's bounded discovery and signed streaming admission;
//! platform replacement is Windows-specific (no macOS daemon or bundle transaction).
use crate::i18n::text;
use serde::Serialize;
use std::sync::Mutex;
use tauri::{AppHandle, Emitter};

#[cfg(windows)]
#[path = "updater_windows.rs"]
mod windows;

/// Shared installed-copy identity for features that require a stable location.
#[cfg(windows)]
pub(crate) fn is_installed() -> bool {
    windows::installed_directory().is_ok()
}

pub const ENDPOINT: Option<&str> = option_env!("LUMATAPE_UPDATE_ENDPOINT");
pub const PUBLIC_KEY: Option<&str> = option_env!("LUMATAPE_UPDATE_PUBLIC_KEY");
const METADATA_LIMIT: usize = 64 * 1024;
const DOWNLOAD_LIMIT: usize = 256 * 1024 * 1024;
const RELEASE_PREFIX: &str = "https://github.com/aiwaki/lumatape/releases/";

#[derive(Clone, Debug, Serialize)]
pub struct Status {
    pub state: String,
    pub current_version: String,
    pub available_version: Option<String>,
    pub reason: Option<String>,
    pub reason_code: Option<String>,
    pub downloaded_bytes: u64,
    pub total_bytes: Option<u64>,
    pub can_install: bool,
}
#[derive(Clone, Debug, PartialEq, Eq)]
struct Offer {
    version: String,
    url: tauri::Url,
    signature: String,
}
struct Record {
    status: Status,
    offer: Option<Offer>,
}
pub struct Updater {
    record: Mutex<Record>,
}
#[derive(Clone, Debug)]
struct Failure {
    code: &'static str,
    message: String,
}
fn fail(code: &'static str, ru: &str, en: &str) -> Failure {
    Failure {
        code,
        message: text(ru, en).into(),
    }
}
fn portable_reason() -> Failure {
    fail("portable_install_disabled", "Обновление на месте доступно для установленной версии. Эта копия не распознана как установленная: скачайте новый ZIP и распакуйте в отдельную папку.", "In-place updates require the installed version. This copy was not recognized as an installed version. Download the new ZIP and extract it to a separate folder.")
}
fn cancelled() -> Failure {
    fail(
        "update_cancelled",
        "Обновление отменено. Можно проверить ещё раз.",
        "Update cancelled. You can check again.",
    )
}
fn busy() -> Failure {
    fail(
        "update_busy",
        "Обновление уже выполняется.",
        "An update operation is already running.",
    )
}
fn inactive_configuration() -> Failure {
    fail("update_channel_unconfigured", "Подписанный канал обновлений ещё не настроен. Эта сборка не проверяет обновления в сети.", "The signed update channel is not configured yet. This build does not check for updates online.")
}
fn reset_progress(status: &mut Status) {
    status.downloaded_bytes = 0;
    status.total_bytes = None;
    status.reason = None;
    status.reason_code = None;
}
/// Dropping an interrupted future must never leave a permanently busy updater.
struct Operation<'a> {
    record: &'a Mutex<Record>,
    done: bool,
}
impl Operation<'_> {
    fn finish(mut self, result: Result<Option<Offer>, Failure>, state: &str) {
        let mut record = self.record.lock().unwrap();
        match result {
            Ok(offer) => {
                record.status.state = state.into();
                record.status.available_version = offer.as_ref().map(|o| o.version.clone());
                record.offer = offer;
                record.status.reason = None;
                record.status.reason_code = None;
                if state == "available" && !record.status.can_install {
                    let reason = portable_reason();
                    record.status.reason = Some(reason.message);
                    record.status.reason_code = Some(reason.code.into());
                }
            }
            Err(error) => set_failure(&mut record, error),
        }
        self.done = true;
    }
}
fn set_failure(record: &mut Record, error: Failure) {
    record.status.state = "error".into();
    record.status.reason = Some(error.message);
    record.status.reason_code = Some(error.code.into());
    record.status.available_version = None;
    record.offer = None;
}
impl Drop for Operation<'_> {
    fn drop(&mut self) {
        if !self.done {
            set_failure(&mut self.record.lock().unwrap(), cancelled());
        }
    }
}
impl Updater {
    pub fn configured(&self) -> bool {
        cfg!(windows)
            && ENDPOINT.is_some_and(|v| !v.is_empty())
            && PUBLIC_KEY.is_some_and(|v| !v.is_empty())
    }
    pub fn new() -> Self {
        let configured = cfg!(windows)
            && ENDPOINT.is_some_and(|v| !v.is_empty())
            && PUBLIC_KEY.is_some_and(|v| !v.is_empty());
        #[cfg(windows)]
        let can_install = windows::installed_directory().is_ok();
        #[cfg(not(windows))]
        let can_install = false;
        let reason = (!configured).then(inactive_configuration);
        Self {
            record: Mutex::new(Record {
                status: Status {
                    state: if configured { "idle" } else { "unconfigured" }.into(),
                    current_version: env!("CARGO_PKG_VERSION").into(),
                    available_version: None,
                    reason: reason.as_ref().map(|e| e.message.clone()),
                    reason_code: reason.map(|e| e.code.into()),
                    downloaded_bytes: 0,
                    total_bytes: None,
                    can_install,
                },
                offer: None,
            }),
        }
    }
    pub fn status(&self) -> Status {
        self.record.lock().unwrap().status.clone()
    }
    fn emit(&self, app: &AppHandle) {
        let _ = app.emit("updater_state", self.status());
    }
    fn begin_check(&self) -> Result<Operation<'_>, Failure> {
        let mut record = self.record.lock().unwrap();
        if matches!(
            record.status.state.as_str(),
            "checking" | "downloading" | "installing"
        ) {
            return Err(busy());
        }
        record.offer = None;
        record.status.available_version = None;
        reset_progress(&mut record.status);
        record.status.state = "checking".into();
        Ok(Operation {
            record: &self.record,
            done: false,
        })
    }
    fn begin_install(&self, version: &str) -> Result<(Operation<'_>, Offer), Failure> {
        let mut record = self.record.lock().unwrap();
        if matches!(
            record.status.state.as_str(),
            "checking" | "downloading" | "installing"
        ) {
            return Err(busy());
        }
        if !record.status.can_install {
            return Err(portable_reason());
        }
        let offer = record
            .offer
            .as_ref()
            .filter(|o| o.version == version && record.status.state == "available")
            .cloned()
            .ok_or_else(|| {
                fail(
                    "update_offer_stale",
                    "Предложение обновления устарело. Проверьте ещё раз.",
                    "The update offer is no longer current. Check again.",
                )
            })?;
        reset_progress(&mut record.status);
        record.status.state = "downloading".into();
        Ok((
            Operation {
                record: &self.record,
                done: false,
            },
            offer,
        ))
    }
    pub async fn check(&self, app: &AppHandle) -> Result<Status, String> {
        if !self.configured() {
            return Ok(self.status());
        }
        let operation = self.begin_check().map_err(|e| e.message)?;
        self.emit(app);
        #[cfg(windows)]
        let result = self.discover().await;
        #[cfg(not(windows))]
        let result: Result<Option<Offer>, Failure> = Err(inactive_configuration());
        let error = result.as_ref().err().map(|e| e.message.clone());
        let state = if matches!(&result, Ok(Some(_))) {
            "available"
        } else {
            "idle"
        };
        operation.finish(result, state);
        self.emit(app);
        match error {
            Some(error) => Err(error),
            None => Ok(self.status()),
        }
    }
    #[cfg(windows)]
    async fn discover(&self) -> Result<Option<Offer>, Failure> {
        let endpoint = valid_endpoint(ENDPOINT.ok_or_else(inactive_configuration)?)?;
        // Fail a malformed embedded trust root before sending any request.
        let _ = decode_key(PUBLIC_KEY.ok_or_else(inactive_configuration)?)?;
        let client = client(std::time::Duration::from_secs(10))?;
        let mut response = client
            .get(endpoint)
            .header("Accept", "application/json")
            .send()
            .await
            .map_err(|_| {
                fail(
                    "update_discovery_network",
                    "Не удалось проверить обновления. Проверьте соединение и повторите.",
                    "Could not check for updates. Check your connection and retry.",
                )
            })?;
        if response.status() == reqwest::StatusCode::NO_CONTENT {
            return Ok(None);
        }
        if response.status() != reqwest::StatusCode::OK {
            return Err(fail(
                "update_discovery_http",
                "Сервер обновлений временно недоступен. Повторите позже.",
                "The update server is unavailable. Retry later.",
            ));
        }
        check_length(response.content_length(), METADATA_LIMIT)?;
        let mut body = Vec::new();
        while let Some(chunk) = response.chunk().await.map_err(|_| {
            fail(
                "update_metadata_read",
                "Не удалось получить список обновлений. Повторите проверку.",
                "Could not read the update index. Check again.",
            )
        })? {
            append_bounded(&mut body, &chunk, METADATA_LIMIT)?;
        }
        let offer = parse_manifest(&body)?;
        if let Some(ref offer) = offer {
            let key = decode_key(PUBLIC_KEY.ok_or_else(inactive_configuration)?)?;
            let signature = decode_signature(&offer.signature)?;
            let _ = SignedCollector::new(&key, &signature)?;
        }
        Ok(offer)
    }
    pub async fn install(
        &self,
        app: &AppHandle,
        version: &str,
        engine: &crate::engine::Engine,
        testcard: &crate::testcard::TestCard,
        terminal: &std::sync::atomic::AtomicU8,
        cancel_epoch: &std::sync::atomic::AtomicU64,
        expected_epoch: u64,
    ) -> Result<Status, String> {
        #[cfg(not(windows))]
        {
            let _ = (
                app,
                version,
                engine,
                testcard,
                terminal,
                cancel_epoch,
                expected_epoch,
            );
            Err(inactive_configuration().message)
        }
        #[cfg(windows)]
        {
            if !self.configured() {
                return Err(inactive_configuration().message);
            }
            let (operation, offered) = self.begin_install(version).map_err(|e| e.message)?;
            self.emit(app);
            let mut engine_shutdown_started = false;
            let result: Result<(), Failure> = async {
                // Bind installation to this exact registered copy, never turn a portable
                // ZIP into a second installed application behind the user's back.
                let directory = windows::installed_directory()?;
                ensure_owner(terminal, cancel_epoch, expected_epoch)?;
                let fresh = await_owned(self.discover(), terminal, cancel_epoch, expected_epoch).await??.ok_or_else(|| fail("update_offer_stale", "Обновление больше не доступно. Проверьте ещё раз.", "The update is no longer available. Check again."))?;
                if fresh != offered { return Err(fail("update_offer_changed", "Обновление изменилось. Проверьте версию ещё раз.", "The update changed. Check the version again.")); }
                let bytes = self.download(app, &fresh, terminal, cancel_epoch, expected_epoch).await?;
                ensure_owner(terminal, cancel_epoch, expected_epoch)?;
                // Disk/identity/PE checks happen before stopping the healthy engine.
                let mut prepared = windows::PreparedInstaller::prepare(app, &bytes, &fresh.version)?;
                drop(bytes);
                ensure_owner(terminal, cancel_epoch, expected_epoch)?;
                testcard.close().await.map_err(|_| fail("update_testcard_cleanup", "Не удалось закрыть тестовое окно. Обновление отменено.", "Could not close the test window. Update cancelled."))?;
                ensure_owner(terminal, cancel_epoch, expected_epoch)?;
                engine_shutdown_started = true;
                engine.stop().await.map_err(|_| fail("update_engine_cleanup", "Не удалось подтвердить восстановление игры. Обновление отменено; проверьте диагностику.", "Could not confirm game restoration. Update cancelled; check diagnostics."))?;
                ensure_owner(terminal, cancel_epoch, expected_epoch)?;
                if windows::installed_directory()? != directory { return Err(fail("update_installation_changed", "Папка установки изменилась. Перезапустите приложение и повторите.", "The installation directory changed. Restart the app and retry.")); }
                claim_handoff(terminal, cancel_epoch, expected_epoch)?;
                prepared.launch(&directory)?;
                Ok(())
            }.await;
            let result = result.map_err(|mut error| {
                if engine_shutdown_started {
                    error.message.push_str(text("\nЭффект остановлен. Перезапустите LumaTape для продолжения игры с эффектом.", "\nThe effect has stopped. Restart LumaTape to use the effect again."));
                }
                error
            });
            let error = result.as_ref().err().map(|e| e.message.clone());
            operation.finish(result.map(|_| Some(offered)), "installing");
            self.emit(app);
            // The host authorizes its normal exit only after a successful process
            // creation. This is launch confirmation, not an installation receipt.
            match error {
                Some(error) => Err(error),
                None => Ok(self.status()),
            }
        }
    }
    #[cfg(windows)]
    async fn download(
        &self,
        app: &AppHandle,
        offer: &Offer,
        terminal: &std::sync::atomic::AtomicU8,
        cancel_epoch: &std::sync::atomic::AtomicU64,
        expected_epoch: u64,
    ) -> Result<Vec<u8>, Failure> {
        let key = decode_key(PUBLIC_KEY.ok_or_else(inactive_configuration)?)?;
        let signature = decode_signature(&offer.signature)?;
        let mut collector = SignedCollector::new(&key, &signature)?;
        let request = client(std::time::Duration::from_secs(120))?
            .get(offer.url.clone())
            .send();
        let mut response = await_owned(request, terminal, cancel_epoch, expected_epoch)
            .await?
            .map_err(|_| {
                fail(
                    "update_download_network",
                    "Не удалось скачать обновление. Можно проверить и повторить.",
                    "Could not download the update. Check again and retry.",
                )
            })?;
        if response.status() != reqwest::StatusCode::OK {
            return Err(fail(
                "update_download_http",
                "Сервер не отдал установщик. Повторите позже.",
                "The server did not return the installer. Retry later.",
            ));
        }
        let total = response.content_length();
        check_length(total, DOWNLOAD_LIMIT)?;
        let mut last = std::time::Instant::now();
        while let Some(chunk) =
            await_owned(response.chunk(), terminal, cancel_epoch, expected_epoch)
                .await?
                .map_err(|_| {
                    fail(
                        "update_download_read",
                        "Загрузка прервалась. Можно проверить и повторить.",
                        "The download was interrupted. Check again and retry.",
                    )
                })?
        {
            ensure_owner(terminal, cancel_epoch, expected_epoch)?;
            collector.append(&chunk)?;
            if last.elapsed() >= std::time::Duration::from_millis(150) {
                self.progress(app, collector.bytes.len(), total);
                last = std::time::Instant::now();
            }
        }
        let bytes = collector.finish()?;
        if total.is_some_and(|expected| expected != bytes.len() as u64) {
            return Err(fail(
                "update_download_truncated",
                "Установщик получен не полностью. Повторите загрузку.",
                "The installer download is incomplete. Retry.",
            ));
        }
        validate_installer(&bytes)?;
        self.progress(app, bytes.len(), total);
        Ok(bytes)
    }
    #[cfg(windows)]
    fn progress(&self, app: &AppHandle, count: usize, total: Option<u64>) {
        {
            let mut record = self.record.lock().unwrap();
            record.status.downloaded_bytes = count as u64;
            record.status.total_bytes = total;
        }
        self.emit(app);
    }
}
#[cfg(any(windows, test))]
fn ensure_owner(
    terminal: &std::sync::atomic::AtomicU8,
    epoch: &std::sync::atomic::AtomicU64,
    expected: u64,
) -> Result<(), Failure> {
    use std::sync::atomic::Ordering;
    if terminal.load(Ordering::Acquire) == 2 && epoch.load(Ordering::Acquire) == expected {
        Ok(())
    } else {
        Err(cancelled())
    }
}
#[cfg(any(windows, test))]
fn claim_handoff(
    terminal: &std::sync::atomic::AtomicU8,
    epoch: &std::sync::atomic::AtomicU64,
    expected: u64,
) -> Result<(), Failure> {
    use std::sync::atomic::Ordering;
    ensure_owner(terminal, epoch, expected)?;
    terminal
        .compare_exchange(2, 3, Ordering::AcqRel, Ordering::Acquire)
        .map_err(|_| cancelled())?;
    // Cancellation increments epoch before changing terminal. Recheck after CAS
    // so an emergency that already began cannot slip between the checks.
    if epoch.load(Ordering::Acquire) != expected || terminal.load(Ordering::Acquire) != 3 {
        return Err(cancelled());
    }
    Ok(())
}
#[cfg(any(windows, test))]
async fn await_owned<F: std::future::Future>(
    future: F,
    terminal: &std::sync::atomic::AtomicU8,
    epoch: &std::sync::atomic::AtomicU64,
    expected: u64,
) -> Result<F::Output, Failure> {
    use std::task::Poll;
    let mut future = std::pin::pin!(future);
    let mut interval = tokio::time::interval(std::time::Duration::from_millis(200));
    std::future::poll_fn(|cx| {
        if let Err(error) = ensure_owner(terminal, epoch, expected) {
            return Poll::Ready(Err(error));
        }
        match future.as_mut().poll(cx) {
            Poll::Ready(output) => Poll::Ready(Ok(output)),
            Poll::Pending => {
                if interval.poll_tick(cx).is_ready() {
                    cx.waker().wake_by_ref();
                }
                Poll::Pending
            }
        }
    })
    .await
}

fn valid_endpoint(address: &str) -> Result<tauri::Url, Failure> {
    if address != format!("{RELEASE_PREFIX}latest/download/latest.json") {
        return Err(fail(
            "update_endpoint_invalid",
            "Адрес канала обновлений настроен неверно.",
            "The update channel address is invalid.",
        ));
    }
    tauri::Url::parse(address).map_err(|_| {
        fail(
            "update_endpoint_invalid",
            "Адрес канала обновлений настроен неверно.",
            "The update channel address is invalid.",
        )
    })
}
fn stable_version(value: &str) -> Result<semver::Version, Failure> {
    let version = semver::Version::parse(value).map_err(|_| {
        fail(
            "update_version_invalid",
            "Сервер вернул неверную версию обновления.",
            "The server returned an invalid update version.",
        )
    })?;
    if !version.pre.is_empty()
        || !version.build.is_empty()
        || [version.major, version.minor, version.patch]
            .iter()
            .any(|part| *part > 65535)
    {
        return Err(fail(
            "update_version_unstable",
            "Этот канал принимает только стабильные выпуски LumaTape для Windows.",
            "This channel accepts only stable LumaTape releases for Windows.",
        ));
    }
    Ok(version)
}
fn valid_offer(version: &str, address: &str) -> Result<tauri::Url, Failure> {
    if stable_version(version)? <= stable_version(env!("CARGO_PKG_VERSION"))? {
        return Err(fail(
            "update_version_old",
            "Обновление должно быть новее установленной версии.",
            "The update must be newer than the installed version.",
        ));
    }
    let expected = format!("{RELEASE_PREFIX}download/v{version}/LumaTape_{version}_x64-setup.exe");
    if address != expected {
        return Err(fail(
            "update_asset_identity",
            "Установщик не соответствует версии LumaTape. Обновление отклонено.",
            "The installer does not match the LumaTape version. Update rejected.",
        ));
    }
    tauri::Url::parse(address).map_err(|_| {
        fail(
            "update_asset_identity",
            "Неверный адрес установщика.",
            "Invalid installer address.",
        )
    })
}
fn parse_manifest(bytes: &[u8]) -> Result<Option<Offer>, Failure> {
    check_length(Some(bytes.len() as u64), METADATA_LIMIT)?;
    let invalid = || {
        fail(
            "update_metadata_invalid",
            "Список обновлений повреждён. Повторите позже.",
            "The update index is invalid. Retry later.",
        )
    };
    let value: serde_json::Value = serde_json::from_slice(bytes).map_err(|_| invalid())?;
    let version = value["version"].as_str().ok_or_else(invalid)?;
    let parsed = stable_version(version)?;
    if parsed <= stable_version(env!("CARGO_PKG_VERSION"))? {
        return Ok(None);
    }
    let target = &value["platforms"]["windows-x86_64"];
    let signature = target["signature"]
        .as_str()
        .filter(|s| !s.is_empty() && s.len() <= 4096)
        .ok_or_else(invalid)?;
    let url = valid_offer(version, target["url"].as_str().ok_or_else(invalid)?)?;
    Ok(Some(Offer {
        version: version.into(),
        url,
        signature: signature.into(),
    }))
}
fn allowed_redirect(next: &tauri::Url, previous: &[tauri::Url]) -> bool {
    previous.len() <= 3
        && !previous.iter().any(|p| p == next)
        && next.scheme() == "https"
        && next.port().is_none()
        && next.username().is_empty()
        && next.password().is_none()
        && next.fragment().is_none()
        && match next.host_str() {
            Some("github.com") => {
                next.as_str().starts_with(RELEASE_PREFIX) && next.query().is_none()
            }
            Some("release-assets.githubusercontent.com" | "objects.githubusercontent.com") => true,
            _ => false,
        }
}
#[cfg(windows)]
fn client(timeout: std::time::Duration) -> Result<reqwest::Client, Failure> {
    reqwest::Client::builder()
        .connect_timeout(std::time::Duration::from_secs(5))
        .timeout(timeout)
        .user_agent(concat!("LumaTape-Updater/", env!("CARGO_PKG_VERSION")))
        .redirect(reqwest::redirect::Policy::custom(|a| {
            if allowed_redirect(a.url(), a.previous()) {
                a.follow()
            } else {
                a.error("Update redirect rejected")
            }
        }))
        .build()
        .map_err(|_| {
            fail(
                "update_network_unavailable",
                "Не удалось подготовить соединение с сервером обновлений.",
                "Could not prepare the update connection.",
            )
        })
}
fn check_length(length: Option<u64>, limit: usize) -> Result<(), Failure> {
    if length.is_some_and(|n| n > limit as u64) {
        Err(fail(
            "update_size_limit",
            "Ответ сервера превысил допустимый размер. Обновление отклонено.",
            "The server response exceeded the size limit. Update rejected.",
        ))
    } else {
        Ok(())
    }
}
fn append_bounded(body: &mut Vec<u8>, chunk: &[u8], limit: usize) -> Result<(), Failure> {
    check_length(Some(body.len().saturating_add(chunk.len()) as u64), limit)?;
    body.extend_from_slice(chunk);
    Ok(())
}
#[cfg(any(windows, test))]
fn decode_text(value: &str) -> Result<String, Failure> {
    use base64::Engine;
    if value.len() > 4096 {
        return Err(fail(
            "update_trust_invalid",
            "Неверный формат подписи обновления.",
            "Invalid update signature format.",
        ));
    }
    base64::engine::general_purpose::STANDARD
        .decode(value)
        .ok()
        .and_then(|b| String::from_utf8(b).ok())
        .ok_or_else(|| {
            fail(
                "update_trust_invalid",
                "Неверный формат подписи обновления.",
                "Invalid update signature format.",
            )
        })
}
#[cfg(any(windows, test))]
fn decode_key(value: &str) -> Result<minisign_verify::PublicKey, Failure> {
    minisign_verify::PublicKey::decode(&decode_text(value)?).map_err(|_| {
        fail(
            "update_key_invalid",
            "Ключ канала обновлений настроен неверно.",
            "The update channel key is invalid.",
        )
    })
}
#[cfg(any(windows, test))]
fn decode_signature(value: &str) -> Result<minisign_verify::Signature, Failure> {
    minisign_verify::Signature::decode(&decode_text(value)?).map_err(|_| {
        fail(
            "update_signature_invalid",
            "Подпись обновления повреждена.",
            "The update signature is invalid.",
        )
    })
}
#[cfg(any(windows, test))]
struct SignedCollector<'a> {
    verifier: minisign_verify::StreamVerifier<'a>,
    bytes: Vec<u8>,
}
#[cfg(any(windows, test))]
impl<'a> SignedCollector<'a> {
    fn new(
        key: &'a minisign_verify::PublicKey,
        signature: &'a minisign_verify::Signature,
    ) -> Result<Self, Failure> {
        Ok(Self {
            verifier: key.verify_stream(signature).map_err(|_| {
                fail(
                    "update_signature_key",
                    "Подпись не соответствует ключу LumaTape.",
                    "The signature does not match the LumaTape key.",
                )
            })?,
            bytes: Vec::new(),
        })
    }
    fn append(&mut self, chunk: &[u8]) -> Result<(), Failure> {
        append_bounded(&mut self.bytes, chunk, DOWNLOAD_LIMIT)?;
        self.verifier.update(chunk);
        Ok(())
    }
    fn finish(mut self) -> Result<Vec<u8>, Failure> {
        self.verifier.finalize().map_err(|_| {
            fail(
                "update_signature_failed",
                "Проверка подписи не пройдена. Установщик не будет запущен.",
                "Signature verification failed. The installer will not run.",
            )
        })?;
        Ok(self.bytes)
    }
}
fn validate_installer(bytes: &[u8]) -> Result<(), Failure> {
    // NSIS bootstrap is a Windows PE even when it installs an x64 application.
    let invalid = || {
        fail(
            "update_installer_invalid",
            "Подписанный файл не является установщиком Windows.",
            "The signed file is not a Windows installer.",
        )
    };
    if bytes.len() < 64 || &bytes[..2] != b"MZ" {
        return Err(invalid());
    }
    let offset = u32::from_le_bytes(bytes[60..64].try_into().unwrap()) as usize;
    if offset < 64 || bytes.get(offset..offset.saturating_add(4)) != Some(&b"PE\0\0"[..]) {
        return Err(invalid());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    fn next() -> String {
        let v = semver::Version::parse(env!("CARGO_PKG_VERSION")).unwrap();
        format!("{}.{}.{}", v.major, v.minor, v.patch + 1)
    }
    fn address(v: &str) -> String {
        format!("{RELEASE_PREFIX}download/v{v}/LumaTape_{v}_x64-setup.exe")
    }
    fn offer() -> Offer {
        Offer {
            version: next(),
            url: tauri::Url::parse(&address(&next())).unwrap(),
            signature: "fixture".into(),
        }
    }
    fn test_updater() -> Updater {
        let u = Updater::new();
        {
            let mut r = u.record.lock().unwrap();
            r.status.state = "idle".into();
            r.status.can_install = true;
        }
        u
    }
    #[test]
    fn updates_default_to_no_network() {
        if ENDPOINT.is_none() || PUBLIC_KEY.is_none() {
            assert_eq!(Updater::new().status().state, "unconfigured");
        }
    }
    #[test]
    fn versions_and_exact_installer_identity_are_bound() {
        assert!(valid_offer(&next(), &address(&next())).is_ok());
        for v in [
            "0.0.0".into(),
            env!("CARGO_PKG_VERSION").into(),
            format!("{}-preview.1", next()),
            format!("{}+local", next()),
        ] {
            assert!(valid_offer(&v, &address(&v)).is_err());
        }
        for url in [
            address(&next()).replace("x64", "arm64"),
            address(&next()).replace("LumaTape_", "Other_"),
            format!("{}?x=1", address(&next())),
            address(&next()).replace("https:", "http:"),
            address(&next()).replace("github.com", "github.com.evil"),
            address(&next()).replace("download/v", "download/v0/../v"),
        ] {
            assert!(valid_offer(&next(), &url).is_err(), "{url}");
        }
    }
    #[test]
    fn metadata_is_bounded_and_requires_exact_target() {
        let manifest = serde_json::json!({"version":next(),"platforms":{"windows-x86_64":{"signature":"fixture","url":address(&next())}}});
        assert_eq!(
            parse_manifest(&serde_json::to_vec(&manifest).unwrap()).unwrap(),
            Some(offer())
        );
        for value in [
            serde_json::json!({"version":next(),"url":address(&next()),"signature":"fixture"}),
            serde_json::json!({"version":next(),"platforms":{"windows-aarch64":manifest["platforms"]["windows-x86_64"]}}),
        ] {
            assert!(parse_manifest(&serde_json::to_vec(&value).unwrap()).is_err());
        }
        assert!(parse_manifest(&vec![b' '; METADATA_LIMIT + 1]).is_err());
        assert!(parse_manifest(b"{").is_err());
        assert!(parse_manifest(br#"{"version":"0.0.0"}"#).unwrap().is_none());
    }
    #[test]
    fn redirects_reject_loops_credentials_untrusted_hosts_and_extra_hops() {
        let previous = vec![tauri::Url::parse(&address(&next())).unwrap()];
        let allowed=tauri::Url::parse("https://release-assets.githubusercontent.com/github-production-release-asset/1?token=fixture").unwrap();
        assert!(allowed_redirect(&allowed, &previous));
        assert!(!allowed_redirect(&allowed, &vec![previous[0].clone(); 4]));
        assert!(!allowed_redirect(&previous[0], &previous));
        for url in [
            "http://github.com/a",
            "https://user@github.com/a",
            "https://github.com:444/a",
            "https://example.com/a",
            "https://github.com/aiwaki/other/releases/x",
            "https://release-assets.githubusercontent.com/a#secret",
        ] {
            assert!(!allowed_redirect(
                &tauri::Url::parse(url).unwrap(),
                &previous
            ));
        }
        assert!(valid_endpoint(&format!("{RELEASE_PREFIX}latest/download/latest.json")).is_ok());
        assert!(valid_endpoint("https://example.com/latest.json").is_err());
    }
    #[test]
    fn failure_invalidates_offer_and_retries_reset_progress() {
        let u = test_updater();
        u.begin_check()
            .unwrap()
            .finish(Ok(Some(offer())), "available");
        let (operation, _) = u.begin_install(&next()).unwrap();
        {
            let mut r = u.record.lock().unwrap();
            r.status.downloaded_bytes = 99;
            r.status.total_bytes = Some(100);
        }
        operation.finish(Err(cancelled()), "installing");
        assert_eq!(u.status().state, "error");
        assert!(u.record.lock().unwrap().offer.is_none());
        let retry = u.begin_check().unwrap();
        assert_eq!(u.status().downloaded_bytes, 0);
        assert!(u.status().total_bytes.is_none());
        retry.finish(Ok(None), "idle");
        assert!(u.status().available_version.is_none());
    }
    #[test]
    fn operations_are_exclusive_and_abandoned_work_can_retry() {
        let u = test_updater();
        let operation = u.begin_check().unwrap();
        assert!(u.begin_check().is_err());
        assert!(u.begin_install(&next()).is_err());
        drop(operation);
        assert_eq!(u.status().reason_code.as_deref(), Some("update_cancelled"));
        u.begin_check()
            .unwrap()
            .finish(Ok(Some(offer())), "available");
        let (operation, _) = u.begin_install(&next()).unwrap();
        assert!(u.begin_install(&next()).is_err());
        assert!(u.begin_check().is_err());
        drop(operation);
        assert!(u.begin_check().is_ok());
    }
    #[test]
    fn portable_cannot_install_and_stale_selection_is_rejected() {
        let u = test_updater();
        u.begin_check()
            .unwrap()
            .finish(Ok(Some(offer())), "available");
        assert!(u.begin_install("99.0.0").is_err());
        u.record.lock().unwrap().status.can_install = false;
        assert_eq!(
            u.begin_install(&next()).err().unwrap().code,
            "portable_install_disabled"
        );
        assert_eq!(u.status().state, "available");
    }
    #[test]
    fn bounded_append_rejects_without_retaining_extra_bytes() {
        let mut body = vec![1, 2];
        append_bounded(&mut body, &[3, 4], 4).unwrap();
        assert!(append_bounded(&mut body, &[5], 4).is_err());
        assert_eq!(body, [1, 2, 3, 4]);
    }
    #[test]
    fn emergency_before_handoff_prevents_launch_ownership() {
        use std::sync::atomic::{AtomicU64, AtomicU8, Ordering};
        let terminal = AtomicU8::new(2);
        let epoch = AtomicU64::new(4);
        assert!(ensure_owner(&terminal, &epoch, 4).is_ok());
        epoch.fetch_add(1, Ordering::AcqRel);
        assert!(claim_handoff(&terminal, &epoch, 4).is_err());
        assert_eq!(terminal.load(Ordering::Acquire), 2);
        terminal.store(4, Ordering::Release);
        assert!(claim_handoff(&terminal, &epoch, 5).is_err());
    }
    #[test]
    fn handoff_is_exclusive_and_after_it_cancellation_cannot_claim_download() {
        use std::sync::atomic::{AtomicU64, AtomicU8, Ordering};
        let terminal = AtomicU8::new(2);
        let epoch = AtomicU64::new(9);
        claim_handoff(&terminal, &epoch, 9).unwrap();
        assert!(terminal
            .compare_exchange(2, 4, Ordering::AcqRel, Ordering::Acquire)
            .is_err());
        assert!(claim_handoff(&terminal, &epoch, 9).is_err());
        assert_eq!(terminal.load(Ordering::Acquire), 3);
    }
    #[test]
    fn stalled_io_cancels_promptly_and_drops_the_pending_future() {
        use std::sync::{
            atomic::{AtomicBool, AtomicU64, AtomicU8, Ordering},
            Arc,
        };
        struct Pending(Arc<AtomicBool>);
        impl std::future::Future for Pending {
            type Output = ();
            fn poll(
                self: std::pin::Pin<&mut Self>,
                _: &mut std::task::Context<'_>,
            ) -> std::task::Poll<()> {
                std::task::Poll::Pending
            }
        }
        impl Drop for Pending {
            fn drop(&mut self) {
                self.0.store(true, Ordering::Release);
            }
        }
        let terminal = Arc::new(AtomicU8::new(2));
        let epoch = Arc::new(AtomicU64::new(1));
        let dropped = Arc::new(AtomicBool::new(false));
        let e = epoch.clone();
        let t = terminal.clone();
        let cancel = std::thread::spawn(move || {
            std::thread::sleep(std::time::Duration::from_millis(40));
            e.fetch_add(1, Ordering::AcqRel);
            t.compare_exchange(2, 4, Ordering::AcqRel, Ordering::Acquire)
                .unwrap();
        });
        let started = std::time::Instant::now();
        let result = tauri::async_runtime::block_on(await_owned(
            Pending(dropped.clone()),
            &terminal,
            &epoch,
            1,
        ));
        cancel.join().unwrap();
        assert_eq!(result.unwrap_err().code, "update_cancelled");
        assert!(dropped.load(Ordering::Acquire));
        assert!(started.elapsed() < std::time::Duration::from_secs(2));
    }
    #[test]
    fn verified_fixtures_and_tampering_share_the_download_boundary() {
        use base64::Engine;
        let f: serde_json::Value =
            serde_json::from_str(include_str!("../tests/fixtures/updater-signed.json")).unwrap();
        let key = decode_key(f["public_key"].as_str().unwrap()).unwrap();
        let signature = decode_signature(f["signature"].as_str().unwrap()).unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(f["payload"].as_str().unwrap())
            .unwrap();
        for chunk_size in [1, 7, bytes.len()] {
            let mut c = SignedCollector::new(&key, &signature).unwrap();
            for chunk in bytes.chunks(chunk_size) {
                c.append(chunk).unwrap();
            }
            assert_eq!(c.finish().unwrap(), bytes);
        }
        let mut changed = bytes.clone();
        changed[30] ^= 1;
        for invalid in [
            bytes[..bytes.len() - 1].to_vec(),
            changed,
            [bytes.as_slice(), &[1]].concat(),
        ] {
            let mut c = SignedCollector::new(&key, &signature).unwrap();
            c.append(&invalid).unwrap();
            assert_eq!(c.finish().unwrap_err().code, "update_signature_failed");
        }
        assert!(decode_key("malformed").is_err());
        assert!(decode_signature("malformed").is_err());
        assert!(validate_installer(&bytes).is_ok());
        assert!(validate_installer(b"signed but not PE").is_err());
        assert_ne!(
            PUBLIC_KEY,
            f["public_key"].as_str(),
            "Fixture key must never be a release trust root"
        );
    }
}
