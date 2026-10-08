//! The complete application surface: a Tauri native menu, without a WebView.
use crate::{
    native_dialog,
    protocol::Request,
    tray_model::{self, Action, Entry, MenuModel},
    Host,
};
use serde_json::{json, Value};
use std::{
    collections::HashMap,
    io::Read,
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        Mutex,
    },
    time::Duration,
};
use tauri::{
    menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu},
    tray::{TrayIcon, TrayIconBuilder},
    AppHandle, Manager,
};

const ID: &str = "lumatape-desktop";
const SERVICE_ID: &str = "service";
fn cancelled() -> &'static str {
    crate::i18n::text(
        "Действие отменено отключением эффекта. Повторите выбор, если он ещё нужен.",
        "Turning off the effect cancelled this action. Select it again if needed.",
    )
}

#[derive(Clone)]
struct Data {
    snapshot: Value,
    sources: Value,
    shaders: Value,
    model: MenuModel,
}
enum NativeItem {
    Plain(MenuItem<tauri::Wry>),
    Check(CheckMenuItem<tauri::Wry>),
}
impl NativeItem {
    fn update(&self, entry: &Entry, busy: bool) -> tauri::Result<()> {
        match self {
            Self::Plain(item) => {
                item.set_text(&entry.label)?;
                item.set_enabled(entry.enabled && !busy)
            }
            Self::Check(item) => {
                item.set_text(&entry.label)?;
                item.set_checked(entry.checked.unwrap_or(false))?;
                item.set_enabled(entry.enabled && !busy)
            }
        }
    }
}
struct Section {
    menu: Submenu<tauri::Wry>,
    ids: Vec<String>,
    items: HashMap<String, NativeItem>,
}
impl Section {
    fn new(app: &AppHandle, label: &str) -> Result<Self, String> {
        Ok(Self {
            menu: Submenu::new(app, label, true).map_err(|e| e.to_string())?,
            ids: vec![],
            items: HashMap::new(),
        })
    }
    fn sync(&mut self, app: &AppHandle, entries: &[Entry], busy: bool) -> Result<(), String> {
        let ids: Vec<_> = entries.iter().map(|e| e.id.clone()).collect();
        if self.ids != ids {
            for old in self.menu.items().map_err(|e| e.to_string())? {
                self.menu.remove(&old).map_err(|e| e.to_string())?;
            }
            self.items.clear();
            for entry in entries {
                let item = match entry.checked {
                    Some(checked) => {
                        let item = CheckMenuItem::with_id(
                            app,
                            &entry.id,
                            &entry.label,
                            entry.enabled && !busy,
                            checked,
                            None::<&str>,
                        )
                        .map_err(|e| e.to_string())?;
                        self.menu.append(&item).map_err(|e| e.to_string())?;
                        NativeItem::Check(item)
                    }
                    None => {
                        let item = MenuItem::with_id(
                            app,
                            &entry.id,
                            &entry.label,
                            entry.enabled && !busy,
                            None::<&str>,
                        )
                        .map_err(|e| e.to_string())?;
                        self.menu.append(&item).map_err(|e| e.to_string())?;
                        NativeItem::Plain(item)
                    }
                };
                self.items.insert(entry.id.clone(), item);
            }
            self.ids = ids;
        } else {
            for entry in entries {
                if let Some(item) = self.items.get(&entry.id) {
                    item.update(entry, busy).map_err(|e| e.to_string())?;
                }
            }
        }
        Ok(())
    }
}
struct Handles {
    icon: TrayIcon,
    menu: Menu<tauri::Wry>,
    status: MenuItem<tauri::Wry>,
    power: MenuItem<tauri::Wry>,
    error: MenuItem<tauri::Wry>,
    confirm: MenuItem<tauri::Wry>,
    updates: MenuItem<tauri::Wry>,
    sources: Section,
    effects: Section,
    settings: Submenu<tauri::Wry>,
    groups: Vec<Section>,
}
impl Handles {
    /// Called only on the main thread. Query real membership so retries after
    /// a failed native update cannot insert a duplicate confirmation item.
    fn sync_confirmation(&self, pending: bool, busy: bool) -> Result<(), String> {
        self.confirm
            .set_enabled(pending && !busy)
            .map_err(|e| e.to_string())?;
        let items = self.menu.items().map_err(|e| e.to_string())?;
        let present = items.iter().any(|item| item.id() == self.confirm.id());
        if pending && !present {
            let position = items
                .iter()
                .position(|item| item.id().as_ref() == SERVICE_ID)
                .ok_or(crate::i18n::text(
                    "Не найден раздел сервисных команд",
                    "The tools menu could not be found",
                ))?;
            self.menu
                .insert(&self.confirm, position)
                .map_err(|e| e.to_string())?;
        } else if !pending && present {
            self.menu.remove(&self.confirm).map_err(|e| e.to_string())?;
        }
        Ok(())
    }
}
pub struct Controller {
    handles: Mutex<Option<Handles>>,
    data: Mutex<Option<Data>>,
    last_error: Mutex<Option<String>>,
    generation: AtomicU64,
    cancel_epoch: AtomicU64,
    emergency_floor: AtomicU64,
    busy: AtomicBool,
    emergency_busy: AtomicBool,
    refreshing: AtomicBool,
    dialog_busy: AtomicBool,
}
impl Controller {
    pub fn new() -> Self {
        Self {
            handles: Mutex::new(None),
            data: Mutex::new(None),
            last_error: Mutex::new(None),
            generation: AtomicU64::new(0),
            cancel_epoch: AtomicU64::new(0),
            emergency_floor: AtomicU64::new(0),
            busy: AtomicBool::new(false),
            emergency_busy: AtomicBool::new(false),
            refreshing: AtomicBool::new(false),
            dialog_busy: AtomicBool::new(false),
        }
    }
    pub fn invalidate(&self) {
        self.generation.fetch_add(1, Ordering::AcqRel);
    }
    pub fn cancel_epoch(&self) -> &AtomicU64 {
        &self.cancel_epoch
    }
}

fn cancel_pending_actions(state: &Host) {
    state.tray.cancel_epoch.fetch_add(1, Ordering::AcqRel);
    // 4 keeps update ownership while a cancelled download/cleanup unwinds.
    // 3 is the irreversible installer handoff; it cannot be cancelled here.
    let _ = state
        .terminal
        .compare_exchange(2, 4, Ordering::AcqRel, Ordering::Acquire);
}
fn item(
    app: &AppHandle,
    id: &str,
    label: &str,
    enabled: bool,
) -> Result<MenuItem<tauri::Wry>, String> {
    MenuItem::with_id(app, id, label, enabled, None::<&str>).map_err(|e| e.to_string())
}
fn build(app: &AppHandle) -> Result<Handles, String> {
    let status = item(
        app,
        "status",
        crate::i18n::text("LumaTape — подключение…", "LumaTape — connecting…"),
        false,
    )?;
    let power = item(
        app,
        "model:power",
        crate::i18n::text("Включить", "Turn on"),
        false,
    )?;
    let error = item(
        app,
        "details",
        crate::i18n::text("Последняя ошибка…", "Last error…"),
        false,
    )?;
    let sources = Section::new(app, crate::i18n::text("Игра", "Game"))?;
    let effects = Section::new(app, crate::i18n::text("Эффект", "Effect"))?;
    let settings = Submenu::new(app, crate::i18n::text("Настройки", "Settings"), true)
        .map_err(|e| e.to_string())?;
    let confirm = item(
        app,
        "confirm",
        crate::i18n::text("Подтвердить видеорежим", "Keep display mode"),
        false,
    )?;
    let import = item(
        app,
        "import",
        crate::i18n::text("Добавить эффект из файла…", "Add effect from file…"),
        true,
    )?;
    let service = Submenu::with_id(app, SERVICE_ID, crate::i18n::text("Сервис", "Tools"), true)
        .map_err(|e| e.to_string())?;
    let updates = item(
        app,
        "updates",
        crate::i18n::text("Обновления…", "Updates…"),
        true,
    )?;
    service
        .append_items(&[
            &item(
                app,
                "testcard",
                crate::i18n::text("Тестовая сцена", "Test scene"),
                true,
            )?,
            &item(
                app,
                "hotkey-test",
                crate::i18n::text("Проверить горячие клавиши…", "Test keyboard shortcuts…"),
                true,
            )?,
            &item(
                app,
                "diagnostics",
                crate::i18n::text("Скопировать диагностику", "Copy diagnostics"),
                true,
            )?,
            &updates,
            &item(
                app,
                "about",
                crate::i18n::text("О LumaTape", "About LumaTape"),
                true,
            )?,
            &error,
        ])
        .map_err(|e| e.to_string())?;
    let sep = || PredefinedMenuItem::separator(app).map_err(|e| e.to_string());
    let menu = Menu::with_items(
        app,
        &[
            &status,
            &power,
            &sep()?,
            &sources.menu,
            &effects.menu,
            &import,
            &sep()?,
            &settings,
            &service,
            &sep()?,
            &item(
                app,
                "version",
                concat!("LumaTape ", env!("CARGO_PKG_VERSION")),
                false,
            )?,
            &item(app, "quit", crate::i18n::text("Выход", "Quit"), true)?,
        ],
    )
    .map_err(|e| e.to_string())?;
    let icon = TrayIconBuilder::with_id(ID)
        .icon(
            app.default_window_icon()
                .ok_or(crate::i18n::text(
                    "Нет иконки приложения",
                    "The application icon is missing",
                ))?
                .clone(),
        )
        .tooltip(crate::i18n::text(
            "LumaTape — подключение…",
            "LumaTape — connecting…",
        ))
        .menu(&menu)
        .show_menu_on_left_click(true)
        .on_menu_event(|app, event| dispatch(app, event.id.as_ref()))
        .build(app)
        .map_err(|e| e.to_string())?;
    Ok(Handles {
        icon,
        menu,
        status,
        power,
        error,
        confirm,
        updates,
        sources,
        effects,
        settings,
        groups: vec![],
    })
}
fn update_menu(status: &crate::updater::Status) -> (String, bool) {
    let label = match status.state.as_str() {
        "checking" => crate::i18n::text("Проверка обновлений…", "Checking for updates…").into(),
        "downloading" => {
            let downloaded = status.downloaded_bytes as f64 / 1_048_576.0;
            match status.total_bytes.filter(|total| *total > 0) {
                Some(total) => {
                    let total = total as f64 / 1_048_576.0;
                    crate::localized!(
                        "Загрузка: {downloaded:.1} / {total:.1} МиБ",
                        "Downloading: {downloaded:.1} / {total:.1} MiB"
                    )
                }
                None => crate::localized!(
                    "Загрузка: {downloaded:.1} МиБ",
                    "Downloading: {downloaded:.1} MiB"
                ),
            }
        }
        "installing" => crate::i18n::text("Установка обновления…", "Installing update…").into(),
        "available" => match status.available_version.as_deref() {
            Some(version) if status.can_install => {
                crate::localized!("Установить версию {version}…", "Install version {version}…")
            }
            Some(version) => {
                crate::localized!("Доступна версия {version}…", "Version {version} available…")
            }
            None => crate::i18n::text("Проверить обновления…", "Check for updates…").into(),
        },
        "error" => {
            crate::i18n::text("Повторить проверку обновлений…", "Retry update check…").into()
        }
        _ => crate::i18n::text("Проверить обновления…", "Check for updates…").into(),
    };
    let enabled = !matches!(
        status.state.as_str(),
        "checking" | "downloading" | "installing"
    );
    (label, enabled)
}

pub fn updater_event(app: &AppHandle) {
    let target = app.clone();
    let _ = app.run_on_main_thread(move || {
        let Some(state) = target.try_state::<Host>() else {
            return;
        };
        // Read on the UI thread: queued progress events must not overwrite a
        // newer terminal/error state with an older snapshot.
        let (label, enabled) = update_menu(&state.updates.status());
        if let Some(handles) = state.tray.handles.lock().unwrap().as_ref() {
            let _ = handles.updates.set_text(label);
            let _ = handles
                .updates
                .set_enabled(enabled && state.terminal.load(Ordering::Acquire) == 0);
        };
    });
}

pub fn engine_event(app: &AppHandle, event: &Value) {
    let Some(state) = app.try_state::<Host>() else {
        return;
    };
    if let Some(sequence) = event["data"]["emergency_sequence"].as_u64() {
        if sequence
            > state
                .tray
                .emergency_floor
                .fetch_max(sequence, Ordering::AcqRel)
        {
            cancel_pending_actions(&state);
            state.tray.invalidate();
        }
    }
}
pub fn install(app: &AppHandle) {
    match build(app) {
        Ok(handles) => *app.state::<Host>().tray.handles.lock().unwrap() = Some(handles),
        Err(error) => {
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                finish_without_tray(
                    &app,
                    &crate::localized!(
                        "Не удалось создать значок. {error}",
                        "Could not create the tray icon. {error}"
                    ),
                )
                .await;
            });
            return;
        }
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let mut misses = 0u8;
        loop {
            if app.state::<Host>().allow_exit.load(Ordering::Acquire) {
                break;
            }
            refresh(&app).await;
            let icon = app
                .state::<Host>()
                .tray
                .handles
                .lock()
                .unwrap()
                .as_ref()
                .map(|h| h.icon.clone());
            #[cfg(windows)]
            let reachable = icon.is_some_and(|icon| icon.rect().ok().flatten().is_some());
            #[cfg(not(windows))]
            let reachable = icon.is_some();
            if reachable {
                misses = 0;
            } else {
                misses = misses.saturating_add(1);
                // Explorer recreates the same tray icon via TaskbarCreated. A
                // lost icon never leaves an inaccessible overlay running.
                if misses == 1 {
                    let _ = engine_call(&app, "emergency", Value::Null).await;
                }
                if misses == 4 {
                    finish_without_tray(&app,crate::i18n::text("Значок в области уведомлений не восстановился после перезапуска панели задач.", "The tray icon did not return after the taskbar restarted.")).await;
                }
            }
            tokio::time::sleep(Duration::from_secs(2)).await;
        }
    });
}
async fn engine_call(app: &AppHandle, kind: &str, payload: Value) -> Result<Value, String> {
    crate::request_engine(
        app,
        Request {
            kind: kind.into(),
            payload,
        },
    )
    .await
}
async fn read_data(app: &AppHandle) -> Result<Data, String> {
    let state = app.state::<Host>();
    let engine = state.engine()?;
    let read = |kind: &str| {
        engine.internal(
            Request {
                kind: kind.into(),
                payload: Value::Null,
            },
            Duration::from_secs(3),
        )
    };
    let snapshot = read("snapshot").await?;
    let sources = read("sources").await?;
    let shaders = read("shaders").await?;
    let model = tray_model::build(&snapshot, &sources, &shaders)?;
    Ok(Data {
        snapshot,
        sources,
        shaders,
        model,
    })
}
async fn refresh(app: &AppHandle) {
    updater_event(app);
    let state = app.state::<Host>();
    if state.tray.refreshing.swap(true, Ordering::AcqRel) {
        return;
    }
    let generation = state.tray.generation.load(Ordering::Acquire);
    let result = read_data(app).await;
    let target = app.clone();
    let _ = app.run_on_main_thread(move || {
        let state = target.state::<Host>();
        if generation != state.tray.generation.load(Ordering::Acquire) {
            return;
        }
        match result {
            Ok(data) => {
                if data.model.emergency_sequence
                    < state.tray.emergency_floor.load(Ordering::Acquire)
                {
                    return;
                }
                state
                    .tray
                    .emergency_floor
                    .fetch_max(data.model.emergency_sequence, Ordering::AcqRel);
                if matches!(
                    data.snapshot["runtime"]["phase"].as_str(),
                    Some("error" | "recovery-error")
                ) {
                    let message = data.snapshot["runtime"]["last_error"]
                        .as_str()
                        .filter(|s| !s.is_empty())
                        .or_else(|| data.snapshot["runtime"]["reason"].as_str())
                        .unwrap_or(
                            crate::i18n::text("Ошибка обработки. Выключите эффект и выберите доступное окно игры.", "Processing failed. Turn off the effect and select an available game window."),
                        );
                    *state.tray.last_error.lock().unwrap() = Some(message.into());
                }
                let mut handles = state.tray.handles.lock().unwrap();
                if let Some(h) = handles.as_mut() {
                    let busy = state.tray.busy.load(Ordering::Acquire);
                    let update = (|| -> Result<(), String> {
                        h.status
                            .set_text(&data.model.status)
                            .map_err(|e| e.to_string())?;
                        h.power
                            .set_text(if busy {
                                crate::i18n::text("Выключить", "Turn off")
                            } else {
                                &data.model.power.label
                            })
                            .map_err(|e| e.to_string())?;
                        h.power
                            .set_enabled(busy || data.model.power.enabled)
                            .map_err(|e| e.to_string())?;
                        h.sync_confirmation(data.model.confirmation_pending, busy)?;
                        h.icon
                            .set_tooltip(Some(format!(
                                "LumaTape — {}",
                                data.model.status.replace("&&", "&")
                            )))
                            .map_err(|e| e.to_string())?;
                        h.sources
                            .menu
                            .set_enabled(true)
                            .map_err(|e| e.to_string())?;
                        h.effects
                            .menu
                            .set_enabled(true)
                            .map_err(|e| e.to_string())?;
                        h.settings.set_enabled(true).map_err(|e| e.to_string())?;
                        h.sources.sync(&target, &data.model.sources, busy)?;
                        h.effects.sync(&target, &data.model.effects, busy)?;
                        if h.groups.is_empty() {
                            for group in &data.model.settings {
                                let section = Section::new(&target, &group.label)?;
                                h.settings
                                    .append(&section.menu)
                                    .map_err(|e| e.to_string())?;
                                h.groups.push(section);
                            }
                        }
                        for (section, group) in h.groups.iter_mut().zip(&data.model.settings) {
                            section.sync(&target, &group.entries, busy)?;
                        }
                        h.error
                            .set_enabled(state.tray.last_error.lock().unwrap().is_some())
                            .map_err(|e| e.to_string())?;
                        Ok(())
                    })();
                    if let Err(error) = update {
                        *state.tray.last_error.lock().unwrap() = Some(error);
                    }
                }
                *state.tray.data.lock().unwrap() = Some(data);
            }
            Err(error) => {
                *state.tray.last_error.lock().unwrap() = Some(friendly_error(&error));
                *state.tray.data.lock().unwrap() = None;
                if let Some(h) = state.tray.handles.lock().unwrap().as_ref() {
                    let _ = h
                        .status
                        .set_text(crate::i18n::text("Движок недоступен — Сервис → Последняя ошибка", "Engine unavailable — Tools → Last error"));
                    let _ = h.error.set_enabled(true);
                    let _ = h.sources.menu.set_enabled(false);
                    let _ = h.effects.menu.set_enabled(false);
                    let _ = h.settings.set_enabled(false);
                    let _ = h.sync_confirmation(false, false);
                    let _ = h.power.set_text(crate::i18n::text("Выключить", "Turn off"));
                    let _ = h.power.set_enabled(true);
                }
            }
        }
    });
    state.tray.refreshing.store(false, Ordering::Release);
}
fn friendly_error(error: &str) -> String {
    serde_json::from_str::<Value>(error)
        .ok()
        .and_then(|v| v["error"]["message"].as_str().map(str::to_owned))
        .unwrap_or_else(|| error.into())
}
pub fn report_error(app: &AppHandle, action: &str, error: &str) {
    let message = format!("{action}\n\n{}", friendly_error(error));
    if let Some(state) = app.try_state::<Host>() {
        *state.tray.last_error.lock().unwrap() = Some(message.clone());
    }
    notify(app, message, true);
}
fn notify(app: &AppHandle, message: String, error: bool) {
    let Some(state) = app.try_state::<Host>() else {
        return;
    };
    if state.tray.dialog_busy.swap(true, Ordering::AcqRel) {
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let _ = tauri::async_runtime::spawn_blocking(move || {
            if error {
                native_dialog::show_error("LumaTape", &message)
            } else {
                native_dialog::show_message("LumaTape", &message)
            }
        })
        .await;
        app.state::<Host>()
            .tray
            .dialog_busy
            .store(false, Ordering::Release);
    });
}
async fn finish_without_tray(app: &AppHandle, reason: &str) {
    // Never force-kill: failure to restore remains visible and retryable even
    // when the tray itself is unavailable. A second launch reopens this path.
    let emergency = engine_call(app, "emergency", Value::Null).await;
    let stopped = if emergency.is_ok() {
        crate::i18n::text(
            "Эффект отключён; выполнено восстановление.",
            "The effect is off; restoration completed.",
        )
        .to_owned()
    } else {
        crate::localized!(
            "Восстановление не подтверждено: {}",
            "Restoration was not confirmed: {}",
            friendly_error(&emergency.unwrap_err())
        )
    };
    let _ = show(crate::localized!(
        "{reason}\n\n{stopped}\nLumaTape попробует безопасно завершиться.",
        "{reason}\n\n{stopped}\nLumaTape will try to shut down safely."
    ))
    .await;
    loop {
        match crate::quit_host(app).await {
            Ok(()) => return,
            Err(error) => {
                *app.state::<Host>().tray.last_error.lock().unwrap() = Some(friendly_error(&error));
                if !confirm(crate::localized!("Не удалось подтвердить восстановление и завершение.\n\n{}\n\nПовторить? Если отменить, следующий запуск LumaTape снова предложит восстановление.", "Restoration and shutdown were not confirmed.\n\n{}\n\nTry again? If you cancel, LumaTape will offer recovery on its next launch.",friendly_error(&error))).await.unwrap_or(false){return}
            }
        }
    }
}
pub fn show_existing(app: &AppHandle) {
    let icon = app.try_state::<Host>().and_then(|s| {
        s.tray
            .handles
            .lock()
            .unwrap()
            .as_ref()
            .map(|h| h.icon.clone())
    });
    #[cfg(windows)]
    let available = icon.is_some_and(|i| i.rect().ok().flatten().is_some());
    #[cfg(not(windows))]
    let available = icon.is_some();
    if available {
        notify(app,crate::i18n::text("LumaTape уже работает. Нажмите на значок в области уведомлений — все команды находятся в его меню.", "LumaTape is already running. Click its tray icon to access all commands.").into(),false);
    } else if let Some(state) = app.try_state::<Host>() {
        if state.tray.busy.swap(true, Ordering::AcqRel) {
            return;
        }
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            finish_without_tray(
                &app,
                crate::i18n::text(
                    "Значок LumaTape недоступен.",
                    "The LumaTape tray icon is unavailable.",
                ),
            )
            .await;
            app.state::<Host>()
                .tray
                .busy
                .store(false, Ordering::Release);
        });
    }
}

fn check_intent(
    app: &AppHandle,
    epoch: u64,
    sequence: Option<u64>,
    data: &Data,
) -> Result<(), String> {
    if epoch
        != app
            .state::<Host>()
            .tray
            .cancel_epoch
            .load(Ordering::Acquire)
        || sequence.is_some_and(|seq| seq != data.model.emergency_sequence)
    {
        Err(cancelled().into())
    } else {
        Ok(())
    }
}
fn dispatch(app: &AppHandle, id: &str) {
    let state = app.state::<Host>();
    let epoch = state.tray.cancel_epoch.load(Ordering::Acquire);
    let cached = state.tray.data.lock().unwrap().clone();
    let sequence = cached.as_ref().map(|d| d.model.emergency_sequence);
    let off = id == "model:power"
        && (state.tray.busy.load(Ordering::Acquire)
            || cached.as_ref().is_none_or(|d| d.model.wants_off));
    if off || id == "quit" {
        cancel_pending_actions(&state);
        if off && state.tray.emergency_busy.swap(true, Ordering::AcqRel) {
            return;
        }
        let quit = id == "quit";
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            let result = if quit {
                crate::quit_host(&app).await
            } else {
                engine_call(&app, "emergency", Value::Null)
                    .await
                    .map(|_| ())
            };
            app.state::<Host>()
                .tray
                .emergency_busy
                .store(false, Ordering::Release);
            if let Err(error) = result {
                report_error(
                    &app,
                    crate::i18n::text(
                        "Не удалось завершить действие",
                        "Could not complete the action",
                    ),
                    &error,
                )
            }
            if !quit {
                refresh(&app).await;
            }
        });
        return;
    }
    if state.tray.busy.swap(true, Ordering::AcqRel) {
        return;
    }
    // Make a pending enable cancellable immediately, before the next snapshot.
    if let Some(h) = state.tray.handles.lock().unwrap().as_ref() {
        let _ = h.power.set_text(crate::i18n::text("Выключить", "Turn off"));
        let _ = h.power.set_enabled(true);
        let _ = h.confirm.set_enabled(false);
    }
    let id = id.to_owned();
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let result = perform(&app, &id, epoch, sequence).await;
        app.state::<Host>()
            .tray
            .busy
            .store(false, Ordering::Release);
        if let Err(error) = result {
            report_error(
                &app,
                crate::i18n::text("Действие не выполнено", "Action failed"),
                &error,
            )
        }
        refresh(&app).await;
    });
}
async fn confirm(message: String) -> Result<bool, String> {
    tauri::async_runtime::spawn_blocking(move || native_dialog::confirm("LumaTape", &message))
        .await
        .map_err(|e| e.to_string())?
}
async fn show(message: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || native_dialog::show_message("LumaTape", &message))
        .await
        .map_err(|e| e.to_string())?
}
async fn copy(text: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || native_dialog::write_clipboard(&text))
        .await
        .map_err(|e| e.to_string())?
}
async fn perform(
    app: &AppHandle,
    id: &str,
    epoch: u64,
    sequence: Option<u64>,
) -> Result<(), String> {
    if let Some(action) = tray_model::parse_action(id) {
        let initial = read_data(app).await?;
        check_intent(app, epoch, sequence, &initial)?;
        let sequence = Some(initial.model.emergency_sequence);
        if matches!(&action,Action::Format(method) if method=="system")&&!confirm(crate::i18n::text("Windows временно сменит видеорежим. Подтвердите результат через меню «Подтвердить видеорежим» до истечения таймера; без подтверждения режим восстановится. Продолжить?", "Windows will temporarily change the display mode. Choose “Keep display mode” before the timer expires, or the previous mode will be restored. Continue?").into()).await?{return Ok(())}
        let data = read_data(app).await?;
        check_intent(app, epoch, sequence, &data)?;
        if action == Action::Power && data.model.wants_off {
            return Err(crate::i18n::text(
                "Состояние изменилось. Откройте меню и повторите команду.",
                "The state changed. Reopen the menu and try again.",
            )
            .into());
        }
        let mutation = tray_model::plan(&action, &data.snapshot, &data.sources, &data.shaders)?;
        engine_call(app, mutation.kind, mutation.payload).await?;
        return Ok(());
    }
    match id {
        "confirm" => {
            let data = read_data(app).await?;
            check_intent(app, epoch, sequence, &data)?;
            engine_call(app, "confirm", Value::Null).await?;
        }
        "import" => import(app, epoch, sequence).await?,
        "diagnostics" => {
            let data = engine_call(app, "diagnostics", Value::Null).await?;
            copy(serde_json::to_string_pretty(&data).map_err(|e| e.to_string())?).await?;
            show(
                crate::i18n::text(
                    "Диагностика скопирована. Ничего не отправлено.",
                    "Diagnostics copied. Nothing was sent.",
                )
                .into(),
            )
            .await?;
        }
        "testcard" => {
            let state = app.state::<Host>();
            let launched = state
                .testcard
                .launch(&state.engine_directory, &state.terminal)?;
            let _ = launched;
        }
        "hotkey-test" => {
            let before = engine_call(app, "snapshot", Value::Null).await?;
            show(crate::localized!("Нажмите назначенные сочетания, затем OK.\n\nПереключение: {}\nОтключение: {}\nЗарегистрированы в Windows: {}\n\nКоманды действуют по-настоящему: переключение меняет эффект, отключение восстанавливает окно.", "Press the assigned shortcuts, then OK.\n\nToggle: {}\nEmergency off: {}\nRegistered in Windows: {}\n\nThese commands are active: toggle changes the effect, emergency off restores the window.",before["config"]["hotkeys"]["toggle"].as_str().unwrap_or("?"),before["config"]["hotkeys"]["emergency"].as_str().unwrap_or("?"),if before["hotkeys"]["registered"]==true{crate::i18n::text("да", "yes")}else{crate::i18n::text("нет", "no")})).await?;
            let after = engine_call(app, "snapshot", Value::Null).await?;
            show(crate::localized!(
                "Получены команды за время проверки:\nПереключение: {}\nОтключение: {}",
                "Commands received during the test:\nToggle: {}\nEmergency off: {}",
                if before["hotkeys"]["toggle_received"] != after["hotkeys"]["toggle_received"] {
                    crate::i18n::text("да", "yes")
                } else {
                    crate::i18n::text("нет", "no")
                },
                if before["hotkeys"]["emergency_received"] != after["hotkeys"]["emergency_received"]
                {
                    crate::i18n::text("да", "yes")
                } else {
                    crate::i18n::text("нет", "no")
                }
            ))
            .await?;
        }
        "updates" => {
            let previous = app.state::<Host>().updates.status();
            let status = if previous.state == "available" {
                previous
            } else {
                app.state::<Host>().updates.check(app).await?
            };
            if let Some(version) = status.available_version {
                if !status.can_install {
                    let reason=status.reason.unwrap_or_default();
                    show(crate::localized!("Доступна версия {version}.\n\n{reason}", "Version {version} is available.\n\n{reason}")).await?;
                }else if confirm(crate::localized!("Доступна версия {version}. Установить её? Эффект будет отключён, изменения окна восстановлены, приложение завершится.", "Version {version} is available. Install it? The effect will turn off, window changes will be restored, and LumaTape will close.")).await?{
                    let data=read_data(app).await?;
                    check_intent(app,epoch,sequence,&data)?;
                    crate::install_update_host(app,&version,epoch).await?;
                }
            } else {
                show(status.reason.unwrap_or_else(|| {
                    crate::i18n::text("Установлена актуальная версия.", "You are up to date.")
                        .into()
                }))
                .await?;
            }
        }
        "details" => {
            let message = app
                .state::<Host>()
                .tray
                .last_error
                .lock()
                .unwrap()
                .clone()
                .unwrap_or_else(|| crate::i18n::text("Ошибок нет.", "No errors.").into());
            show(message).await?;
        }
        "about" => tauri::async_runtime::spawn_blocking(native_dialog::open_repository)
            .await
            .map_err(|error| error.to_string())??,
        _ => {
            return Err(
                crate::i18n::text("Неизвестная команда меню", "Unknown menu command").into(),
            )
        }
    }
    Ok(())
}
async fn import(app: &AppHandle, epoch: u64, sequence: Option<u64>) -> Result<(), String> {
    let initial = read_data(app).await?;
    check_intent(app, epoch, sequence, &initial)?;
    let sequence = Some(initial.model.emergency_sequence);
    let source = tauri::async_runtime::spawn_blocking(|| -> Result<Option<String>, String> {
        let Some(path) = native_dialog::pick_shader_file()? else {
            return Ok(None);
        };
        let mut bytes = Vec::new();
        std::fs::File::open(path)
            .map_err(|e| e.to_string())?
            .take(65537)
            .read_to_end(&mut bytes)
            .map_err(|e| e.to_string())?;
        if bytes.len() > 65536 {
            return Err(crate::i18n::text(
                "Файл больше 64 КиБ. Попросите AI сократить код.",
                "The file exceeds 64 KiB. Ask your AI to shorten the code.",
            )
            .into());
        }
        String::from_utf8(bytes).map(Some).map_err(|_| {
            crate::i18n::text(
                "Файл должен быть текстом UTF-8.",
                "The file must contain UTF-8 text.",
            )
            .into()
        })
    })
    .await
    .map_err(|e| e.to_string())??;
    let Some(source) = source else { return Ok(()) };
    let before = read_data(app).await?;
    check_intent(app, epoch, sequence, &before)?;
    let imported = engine_call(app, "shader_import", json!({"source":source})).await?;
    let data = read_data(app).await?;
    check_intent(app, epoch, sequence, &data)?;
    if data.snapshot["source"].is_null() {
        show(
            crate::i18n::text(
                "Эффект добавлен в меню. Выберите игру, затем добавленный эффект.",
                "Effect added to the menu. Select a game, then your new effect.",
            )
            .into(),
        )
        .await?;
        return Ok(());
    }
    if imported["shader"]["coordinates"] == "warp"
        && data.snapshot["config"]["input_mode"] != "keyboard-gamepad"
    {
        show(crate::i18n::text("Эффект добавлен в меню. Он искривляет изображение: для его выбора установите «Настройки → Искажения изображения → Разрешить произвольные искажения». Клики могут не совпадать с изображением.", "Effect added to the menu. It warps the image: choose “Settings → Image distortion → Allow arbitrary distortion” to use it. Clicks may not align with the image.").into()).await?;
        return Ok(());
    }
    let shader = imported["shader"]["id"].as_str().ok_or(crate::i18n::text(
        "Движок не вернул идентификатор эффекта",
        "The engine did not return an effect identifier",
    ))?;
    let mutation = tray_model::plan(
        &Action::Shader(shader.into()),
        &data.snapshot,
        &data.sources,
        &data.shaders,
    )?;
    engine_call(app, mutation.kind, mutation.payload).await?;
    Ok(())
}

#[cfg(test)]
mod update_menu_tests {
    use super::update_menu;
    use crate::i18n::{with_language, Language};

    #[test]
    fn update_menu_preserves_state_and_portable_boundary_in_both_languages() {
        for language in [Language::Ru, Language::En] {
            with_language(language, || {
                let mut status = crate::updater::Updater::new().status();
                status.state = "available".into();
                status.available_version = Some("0.4.0".into());
                status.can_install = false;
                let (portable, enabled) = update_menu(&status);
                assert!(enabled && portable.contains("0.4.0"));
                status.can_install = true;
                assert_ne!(portable, update_menu(&status).0);
                for state in ["checking", "downloading", "installing"] {
                    status.state = state.into();
                    assert!(!update_menu(&status).1, "{state}");
                }
                status.state = "downloading".into();
                status.downloaded_bytes = 2 * 1_048_576;
                status.total_bytes = None;
                let unknown_total = update_menu(&status).0;
                assert!(unknown_total.contains("2.0"));
                assert!(!unknown_total.contains('/'));
                status.total_bytes = Some(4 * 1_048_576);
                assert!(update_menu(&status).0.contains("2.0 / 4.0"));
                status.state = "error".into();
                let failure = update_menu(&status);
                assert!(failure.1);
                status.state = "unconfigured".into();
                let unconfigured = update_menu(&status);
                assert!(unconfigured.1);
                assert_ne!(failure.0, unconfigured.0);
                assert!(!unconfigured.0.contains("0.4.0"));
            });
        }
    }
}
