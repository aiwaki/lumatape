//! Explicit, installed-copy-only Windows login startup.
//! Reads never register the app or override a Windows startup preference.
use crate::i18n::text;
use std::sync::{
    atomic::{AtomicU8, Ordering},
    Mutex,
};

// Exercise the exact vendored command builder in the regular, side-effect-free
// host test suite, including when running it on macOS.
#[cfg(test)]
#[path = "../vendor/auto-launch/src/windows_command.rs"]
mod windows_command_tests;

#[cfg(all(windows, test))]
#[path = "autostart_registry_tests.rs"]
mod windows_tests;

#[cfg(windows)]
pub const ENTRY_NAME: &str = "LumaTape";

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Status {
    pub enabled: bool,
    pub can_toggle: bool,
    pub reason: Option<String>,
    pub last_error: Option<String>,
}
impl Status {
    fn unavailable(reason: &str) -> Self {
        Self {
            enabled: false,
            can_toggle: false,
            reason: Some(reason.into()),
            last_error: None,
        }
    }
    #[cfg(any(windows, test))]
    fn ready(enabled: bool) -> Self {
        Self {
            enabled,
            can_toggle: true,
            reason: None,
            last_error: None,
        }
    }
}

#[derive(Default)]
pub struct Controller {
    // Serializes both reads and toggles. A background refresh cannot discard a
    // failed user operation; only a subsequent successful operation clears it.
    last_error: Mutex<Option<String>>,
}
impl Controller {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn status(&self, app: &tauri::AppHandle) -> Status {
        let last_error = self.last_error.lock().unwrap();
        #[cfg(windows)]
        let result = WindowsBackend { app }.read();
        #[cfg(not(windows))]
        let result = {
            let _ = app;
            Ok::<_, String>(unsupported())
        };
        status_result(result, &last_error)
    }

    pub fn toggle(&self, app: &tauri::AppHandle, terminal: &AtomicU8) -> Result<Status, String> {
        self.run_toggle(terminal, || {
            #[cfg(windows)]
            return toggle_backend(&mut WindowsBackend { app });
            #[cfg(not(windows))]
            {
                let _ = app;
                Err(unsupported().reason.unwrap())
            }
        })
    }

    fn run_toggle(
        &self,
        terminal: &AtomicU8,
        operation: impl FnOnce() -> Result<Status, String>,
    ) -> Result<Status, String> {
        let mut last_error = self.last_error.lock().unwrap();
        // A queued click can wait behind a refresh while quit/update begins.
        // Check only after obtaining the same lock that covers registry writes.
        let result = if terminal.load(Ordering::Acquire) != 0 {
            Err(text(
                "LumaTape закрывается или обновляется.",
                "LumaTape is closing or updating.",
            )
            .into())
        } else {
            operation()
        };
        finish_toggle(result, &mut last_error)
    }

    /// After claiming terminal ownership, wait on a blocking worker before
    /// stopping the engine or handing off to the installer. New toggles reject
    /// that terminal state; this drains a mutation that was already in progress.
    pub fn wait_idle(&self) {
        drop(self.last_error.lock().unwrap());
    }
}

fn status_result(result: Result<Status, String>, last_error: &Option<String>) -> Status {
    let mut status = result.unwrap_or_else(|error| Status::unavailable(&error));
    status.last_error = last_error.clone();
    status
}

fn finish_toggle(
    result: Result<Status, String>,
    last_error: &mut Option<String>,
) -> Result<Status, String> {
    *last_error = result.as_ref().err().cloned();
    result
}

#[cfg(not(windows))]
fn unsupported() -> Status {
    Status::unavailable(text(
        "Автозапуск доступен в установленной версии для Windows.",
        "Startup is available in the installed Windows version.",
    ))
}

#[cfg(any(windows, test))]
trait Backend {
    fn read(&self) -> Result<Status, String>;
    fn set_enabled(&mut self, enabled: bool) -> Result<(), String>;
}

#[cfg(any(windows, test))]
fn toggle_backend(backend: &mut impl Backend) -> Result<Status, String> {
    let before = backend.read()?;
    if !before.can_toggle {
        return Err(before
            .reason
            .unwrap_or_else(|| text("Автозапуск недоступен.", "Startup is unavailable.").into()));
    }
    let target = !before.enabled;
    backend.set_enabled(target)?;
    let after = backend.read()?;
    if !after.can_toggle || after.enabled != target {
        return Err(text(
            "Windows не подтвердил изменение автозапуска. Проверьте раздел «Автозагрузка» в Диспетчере задач.",
            "Windows did not confirm the startup change. Check Startup apps in Task Manager.",
        )
        .into());
    }
    Ok(after)
}

/// Only our quoted executable, without arguments, can own this entry. In
/// particular, an unquoted path, another executable, or extra args are foreign.
#[cfg(any(windows, test))]
fn command_path(command: &str) -> Option<&str> {
    let path = command.trim_end().strip_prefix('"')?.strip_suffix('"')?;
    (!path.is_empty() && !path.contains(['"', '\0', '\r', '\n'])).then_some(path)
}

#[cfg(any(windows, test))]
fn run_string(bytes: &[u8]) -> Option<String> {
    // A malformed REG_SZ must not become a different, apparently owned path
    // through winreg's lossy UTF-16 conversion or trailing-NUL trimming.
    if bytes.len() < 2 || bytes.len() > 522 || bytes.len() % 2 != 0 {
        return None;
    }
    let words: Vec<u16> = bytes
        .chunks_exact(2)
        .map(|pair| u16::from_le_bytes([pair[0], pair[1]]))
        .collect();
    let (&0, value) = words.split_last()? else {
        return None;
    };
    if value.contains(&0) {
        return None;
    }
    String::from_utf16(value).ok()
}

#[cfg(any(windows, test))]
fn supported_command_length(executable: &str) -> bool {
    // Windows Run documents a 260-character command limit. The patched backend
    // adds two quotes and one trailing space, with no arguments.
    executable.encode_utf16().count() + 3 <= 260
}

/// StartupApproved is Windows-owned, so do not infer approval from a zero
/// timestamp (auto-launch 0.5.0's heuristic). Accept the known enabled record;
/// other records remain unchecked until the user explicitly enables startup.
#[cfg(any(windows, test))]
fn startup_approved(value: Option<&[u8]>) -> bool {
    value.is_none_or(|value| {
        value.len() == 12 && value[..4] == [2, 0, 0, 0] && value[4..].iter().all(|&byte| byte == 0)
    })
}

#[cfg(windows)]
pub fn plugin() -> tauri::plugin::TauriPlugin<tauri::Wry> {
    // Match Tauri NSIS's product-name-based uninstall cleanup. Merely installing
    // the plugin does not create a Run entry.
    tauri_plugin_autostart::Builder::new()
        .app_name(ENTRY_NAME)
        .build()
}

#[cfg(windows)]
struct WindowsBackend<'a> {
    app: &'a tauri::AppHandle,
}

#[cfg(windows)]
mod windows_registry {
    use super::*;
    use std::io;
    use winreg::{
        enums::{HKEY_CURRENT_USER, KEY_READ, KEY_WOW64_64KEY, REG_BINARY, REG_SZ},
        RegKey, RegValue,
    };

    pub(super) const RUN: &str = r"Software\Microsoft\Windows\CurrentVersion\Run";
    pub(super) const APPROVED: &str =
        r"Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run";

    fn read_value(key: &str) -> io::Result<Option<RegValue>> {
        let value = RegKey::predef(HKEY_CURRENT_USER)
            .open_subkey_with_flags(key, KEY_READ | KEY_WOW64_64KEY)
            .and_then(|key| key.get_raw_value(ENTRY_NAME));
        match value {
            Ok(value) => Ok(Some(value)),
            Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
            Err(error) => Err(error),
        }
    }

    fn registry_error(error: impl std::fmt::Display) -> String {
        format!(
            "{} {error}",
            text(
                "Не удалось прочитать или изменить автозапуск Windows.",
                "Could not read or change Windows startup."
            )
        )
    }

    impl WindowsBackend<'_> {
        fn owned_status(&self) -> Result<Status, String> {
            if !crate::updater::is_installed() {
                return Ok(Status::unavailable(text(
                    "Автозапуск доступен после установки LumaTape. Переносная копия не меняет автозагрузку.",
                    "Install LumaTape to enable startup. A portable copy cannot change startup apps.",
                )));
            }
            let executable = std::env::current_exe().map_err(registry_error)?;
            let Some(executable_text) = executable.to_str() else {
                return Ok(Status::unavailable(text(
                    "Путь к LumaTape не подходит для автозапуска Windows.",
                    "The LumaTape path cannot be used for Windows startup.",
                )));
            };
            if !supported_command_length(executable_text) {
                return Ok(Status::unavailable(text(
                    "Путь к LumaTape слишком длинный для автозапуска Windows. Установите приложение в папку с более коротким путём.",
                    "The LumaTape path is too long for Windows startup. Install the app in a shorter path.",
                )));
            }
            let Some(run) = read_value(RUN).map_err(registry_error)? else {
                return Ok(Status::ready(false));
            };
            let current = std::env::current_exe()
                .and_then(|path| path.canonicalize())
                .map_err(registry_error)?;
            let owned = run.vtype == REG_SZ
                && run_string(&run.bytes)
                    .and_then(|command| {
                        command_path(&command).and_then(|path| {
                            let path = std::path::Path::new(path);
                            path.is_absolute()
                                .then(|| path.canonicalize().ok())
                                .flatten()
                        })
                    })
                    .is_some_and(|path| path == current);
            if !owned {
                return Ok(Status::unavailable(text(
                    "Запись автозапуска LumaTape принадлежит другой копии или была изменена. Проверьте её в Windows.",
                    "The LumaTape startup entry belongs to another copy or was changed. Check it in Windows.",
                )));
            }
            let approval = read_value(APPROVED).map_err(registry_error)?;
            let approved = approval.as_ref().is_none_or(|value| {
                value.vtype == REG_BINARY && startup_approved(Some(&value.bytes))
            });
            let mut status = Status::ready(approved);
            if !approved {
                status.reason = Some(text(
                    "Windows не подтверждает автозапуск. Повторное включение разрешит запуск при входе.",
                    "Windows has not approved startup. Enabling it again allows launch at sign-in.",
                ).into());
            }
            Ok(status)
        }
    }

    impl Backend for WindowsBackend<'_> {
        fn read(&self) -> Result<Status, String> {
            self.owned_status()
        }

        fn set_enabled(&mut self, enabled: bool) -> Result<(), String> {
            // Recheck ownership immediately before mutation. The controller
            // mutex also serializes all in-process refresh and menu operations.
            let status = self.owned_status()?;
            if !status.can_toggle {
                return Err(status.reason.unwrap());
            }
            use tauri_plugin_autostart::ManagerExt;
            if enabled {
                self.app.autolaunch().enable().map_err(registry_error)
            } else {
                self.app.autolaunch().disable().map_err(registry_error)
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;

    struct FakeBackend {
        reads: RefCell<std::collections::VecDeque<Result<Status, String>>>,
        writes: Vec<bool>,
        failure: Option<String>,
    }
    impl FakeBackend {
        fn new(reads: impl IntoIterator<Item = Result<Status, String>>) -> Self {
            Self {
                reads: RefCell::new(reads.into_iter().collect()),
                writes: Vec::new(),
                failure: None,
            }
        }
    }
    impl Backend for FakeBackend {
        fn read(&self) -> Result<Status, String> {
            self.reads.borrow_mut().pop_front().unwrap()
        }
        fn set_enabled(&mut self, enabled: bool) -> Result<(), String> {
            self.writes.push(enabled);
            self.failure.clone().map_or(Ok(()), Err)
        }
    }

    #[test]
    fn accepts_only_a_quoted_command_without_extra_arguments() {
        assert_eq!(
            command_path(r#""C:\Игры и программы\LumaTape\lumatape.exe" "#),
            Some(r"C:\Игры и программы\LumaTape\lumatape.exe")
        );
        for command in [
            r"C:\Program Files\LumaTape\lumatape.exe",
            r#""C:\Apps\lumatape.exe" --other"#,
            r#""C:\Apps\lumatape.exe" "other""#,
            "\"\"",
            "\"C:\\Apps\\bad\0name.exe\"",
        ] {
            assert_eq!(command_path(command), None, "{command:?}");
        }
    }

    #[test]
    fn windows_disabled_or_unrecognized_override_never_looks_enabled() {
        assert!(startup_approved(None));
        let mut bytes = [0; 12];
        bytes[0] = 2;
        assert!(startup_approved(Some(&bytes)));
        bytes[0] = 3;
        assert!(!startup_approved(Some(&bytes))); // Even with a zero timestamp.
        bytes[4] = 1;
        assert!(!startup_approved(Some(&bytes)));
        assert!(!startup_approved(Some(&[])));
        assert!(!startup_approved(Some(&[0; 12])));
    }

    #[test]
    fn malformed_registry_strings_cannot_acquire_ownership() {
        let command = r#""C:\Игры\lumatape.exe" "#;
        let bytes: Vec<_> = command
            .encode_utf16()
            .chain(Some(0))
            .flat_map(u16::to_le_bytes)
            .collect();
        assert_eq!(run_string(&bytes).as_deref(), Some(command));
        assert!(run_string(&bytes[..bytes.len() - 2]).is_none());
        assert!(run_string(&[0, 0, 0, 0]).is_none());
        assert!(run_string(&[0, 0xd8, 0, 0]).is_none());
        assert!(run_string(&[0, 0, 0]).is_none());
        assert!(run_string(&[0; 524]).is_none());
    }

    #[test]
    fn run_command_limit_counts_quotes_and_utf16_characters() {
        assert!(supported_command_length(&"x".repeat(257)));
        assert!(!supported_command_length(&"x".repeat(258)));
        assert!(supported_command_length(&"😀".repeat(128)));
        assert!(!supported_command_length(&"😀".repeat(129)));
    }

    #[test]
    fn fresh_read_drives_toggle_and_success_requires_readback() {
        for initial in [false, true] {
            let mut backend =
                FakeBackend::new([Ok(Status::ready(initial)), Ok(Status::ready(!initial))]);
            assert_eq!(toggle_backend(&mut backend).unwrap().enabled, !initial);
            assert_eq!(backend.writes, [!initial]);
        }
        let mut backend = FakeBackend::new([Ok(Status::ready(false)), Ok(Status::ready(false))]);
        assert!(toggle_backend(&mut backend).is_err());
    }

    #[test]
    fn foreign_or_portable_entry_and_read_errors_never_mutate() {
        for before in [
            Ok(Status::unavailable("different owner")),
            Err("read denied".into()),
        ] {
            let mut backend = FakeBackend::new([before]);
            assert!(toggle_backend(&mut backend).is_err());
            assert!(backend.writes.is_empty());
        }
    }

    #[test]
    fn mutation_error_survives_background_refresh_until_successful_retry() {
        let mut backend = FakeBackend::new([Ok(Status::ready(false))]);
        backend.failure = Some("write denied".into());
        let mut error = None;
        assert!(finish_toggle(toggle_backend(&mut backend), &mut error).is_err());
        let refreshed = status_result(Ok(Status::ready(false)), &error);
        assert_eq!(refreshed.last_error.as_deref(), Some("write denied"));
        assert_eq!(
            status_result(Err("read denied".into()), &error).last_error,
            error
        );
        // A backend may write Run before failing on StartupApproved. A refresh
        // must show the actual state and retain the partial-operation error.
        let partially_changed = status_result(Ok(Status::ready(true)), &error);
        assert!(partially_changed.enabled);
        assert_eq!(partially_changed.last_error, error);
        finish_toggle(Ok(Status::ready(true)), &mut error).unwrap();
        assert!(error.is_none());
    }

    #[test]
    fn queued_toggle_rechecks_terminal_after_acquiring_the_registry_lock() {
        use std::sync::{mpsc, Arc, Barrier};
        let controller = Arc::new(Controller::new());
        let terminal = Arc::new(AtomicU8::new(0));
        let locked = controller.last_error.lock().unwrap();
        let ready = Arc::new(Barrier::new(2));
        let (sent, received) = mpsc::channel();
        let child = {
            let controller = controller.clone();
            let terminal = terminal.clone();
            let ready = ready.clone();
            std::thread::spawn(move || {
                ready.wait();
                let mut backend =
                    FakeBackend::new([Ok(Status::ready(false)), Ok(Status::ready(true))]);
                let result = controller.run_toggle(&terminal, || toggle_backend(&mut backend));
                sent.send((result, backend.writes)).unwrap();
            })
        };
        ready.wait();
        assert!(matches!(
            received.recv_timeout(std::time::Duration::from_millis(50)),
            Err(mpsc::RecvTimeoutError::Timeout)
        ));
        terminal.store(1, Ordering::Release);
        drop(locked);
        let (result, writes) = received
            .recv_timeout(std::time::Duration::from_secs(2))
            .unwrap();
        assert!(result.is_err());
        assert!(writes.is_empty());
        child.join().unwrap();
    }

    #[test]
    fn terminal_drain_waits_for_an_in_progress_registry_operation() {
        use std::sync::{mpsc, Arc, Barrier};
        let controller = Arc::new(Controller::new());
        let locked = controller.last_error.lock().unwrap();
        let ready = Arc::new(Barrier::new(2));
        let (sent, received) = mpsc::channel();
        let child = {
            let controller = controller.clone();
            let ready = ready.clone();
            std::thread::spawn(move || {
                ready.wait();
                controller.wait_idle();
                sent.send(()).unwrap();
            })
        };
        ready.wait();
        assert!(matches!(
            received.recv_timeout(std::time::Duration::from_millis(50)),
            Err(mpsc::RecvTimeoutError::Timeout)
        ));
        drop(locked);
        received
            .recv_timeout(std::time::Duration::from_secs(2))
            .unwrap();
        child.join().unwrap();
    }
}
