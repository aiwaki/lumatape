//! A single explicitly launched, fixed bundled diagnostic window; no shell API.
use serde::Serialize;
use std::{
    path::Path,
    process::Child,
    sync::{
        atomic::{AtomicU8, Ordering},
        Mutex,
    },
    time::Duration,
};

#[derive(Serialize)]
pub struct Launched {
    pub started: bool,
    pub pid: u32,
}
pub struct TestCard(Mutex<Option<Child>>);
impl TestCard {
    pub fn new() -> Self {
        Self(Mutex::new(None))
    }
    pub fn launch(&self, directory: &Path, terminal: &AtomicU8) -> Result<Launched, String> {
        #[cfg(not(windows))]
        {
            let _ = (directory, terminal);
            Err(crate::i18n::text(
                "Тестовая игра доступна в Windows.",
                "The test scene is available on Windows.",
            )
            .into())
        }
        #[cfg(windows)]
        {
            use std::{
                os::windows::process::CommandExt,
                process::{Command, Stdio},
            };
            let mut owned = self.0.lock().unwrap();
            // Check under the same lock used by close(): no launch after its last check.
            if terminal.load(Ordering::Acquire) != 0 {
                return Err(crate::i18n::text(
                    "Выполняется завершение или установка обновления",
                    "A terminal operation is in progress",
                )
                .into());
            }
            if let Some(child) = owned.as_mut() {
                if child.try_wait().map_err(|e| e.to_string())?.is_none() {
                    let pid = child.id();
                    native::windows_for(pid, false);
                    return Ok(Launched {
                        started: false,
                        pid,
                    });
                }
                *owned = None;
            }
            let executable = directory.join("lumatape-testcard.exe");
            if !executable.is_file() {
                return Err(crate::i18n::text(
                    "В сборке отсутствует lumatape-testcard.exe.",
                    "The package is missing lumatape-testcard.exe.",
                )
                .into());
            }
            let child = Command::new(executable)
                .arg("--color-field")
                .env("LUMATAPE_UI_LANGUAGE", crate::i18n::language().code())
                .current_dir(directory)
                .stdin(Stdio::null())
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .creation_flags(0x08000000)
                .spawn()
                .map_err(|e| {
                    crate::localized!(
                        "Не удалось открыть тестовую игру: {e}",
                        "Could not open the test scene: {e}"
                    )
                })?;
            let pid = child.id();
            *owned = Some(child);
            Ok(Launched { started: true, pid })
        }
    }
    pub async fn close(&self) -> Result<(), String> {
        #[cfg(windows)]
        {
            let pid = {
                let mut owned = self.0.lock().unwrap();
                let Some(child) = owned.as_mut() else {
                    return Ok(());
                };
                if child.try_wait().map_err(|e| e.to_string())?.is_some() {
                    *owned = None;
                    return Ok(());
                }
                child.id()
            };
            native::windows_for(pid, true);
            let deadline = tokio::time::Instant::now() + Duration::from_secs(4);
            loop {
                {
                    let mut owned = self.0.lock().unwrap();
                    let Some(child) = owned.as_mut() else {
                        return Ok(());
                    };
                    if child.try_wait().map_err(|e| e.to_string())?.is_some() {
                        *owned = None;
                        return Ok(());
                    }
                }
                if tokio::time::Instant::now() >= deadline {
                    return Err(crate::i18n::text(
                        "Закройте тестовую игру и повторите действие; её процесс ещё работает.",
                        "Close the test scene and try again; its process is still running.",
                    )
                    .into());
                }
                tokio::time::sleep(Duration::from_millis(25)).await;
            }
        }
        #[cfg(not(windows))]
        Ok(())
    }
}
#[cfg(windows)]
mod native {
    use std::ffi::c_void;
    type Hwnd = *mut c_void;
    #[link(name = "user32")]
    extern "system" {
        fn EnumWindows(callback: unsafe extern "system" fn(Hwnd, isize) -> i32, data: isize)
            -> i32;
        fn GetWindowThreadProcessId(hwnd: Hwnd, pid: *mut u32) -> u32;
        fn PostMessageW(hwnd: Hwnd, message: u32, wparam: usize, lparam: isize) -> i32;
        fn ShowWindow(hwnd: Hwnd, command: i32) -> i32;
        fn SetForegroundWindow(hwnd: Hwnd) -> i32;
    }
    struct Action {
        pid: u32,
        close: bool,
    }
    unsafe extern "system" fn each(hwnd: Hwnd, context: isize) -> i32 {
        let action = &*(context as *const Action);
        let mut pid = 0;
        GetWindowThreadProcessId(hwnd, &mut pid);
        if pid == action.pid {
            if action.close {
                PostMessageW(hwnd, 0x0010, 0, 0); // WM_CLOSE; never terminate an external PID.
            } else {
                ShowWindow(hwnd, 9); // SW_RESTORE
                SetForegroundWindow(hwnd);
            }
        }
        1
    }
    pub fn windows_for(pid: u32, close: bool) {
        let action = Action { pid, close };
        unsafe {
            EnumWindows(each, &action as *const Action as isize);
        }
    }
}
