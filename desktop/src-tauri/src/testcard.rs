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
                    "LumaTape закрывается или обновляется.",
                    "LumaTape is closing or updating.",
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
            self.close_with(Duration::from_secs(4), |pid| native::windows_for(pid, true))
                .await
        }
        #[cfg(not(windows))]
        Ok(())
    }
    #[cfg(any(windows, test))]
    async fn close_with(
        &self,
        timeout: Duration,
        mut close_windows: impl FnMut(u32),
    ) -> Result<(), String> {
        let deadline = tokio::time::Instant::now() + timeout;
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
                // Process creation precedes HWND creation. Retry until the
                // window appears, keeping the owned process handle pinned while
                // enumerating so another closer cannot release/reuse its PID.
                close_windows(child.id());
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::{
        io::{BufRead, BufReader, Read, Write},
        process::{ChildStdin, Command, Stdio},
        sync::{atomic::AtomicBool, mpsc, Arc},
        thread::JoinHandle,
    };

    // A real child waits for the first close attempt before becoming ready.
    // This makes the startup ordering deterministic on slow/loaded builders.
    struct Fixture {
        card: TestCard,
        input: ChildStdin,
        ready: Arc<AtomicBool>,
        output: JoinHandle<String>,
    }
    impl Fixture {
        fn new(mode: &str) -> Self {
            let mut child = Command::new(std::env::current_exe().unwrap())
                .args(["--exact", "testcard::tests::close_fixture", "--nocapture"])
                .env("LUMATAPE_TEST_CLOSE_FIXTURE", mode)
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null())
                .spawn()
                .unwrap();
            let input = child.stdin.take().unwrap();
            let stdout = child.stdout.take().unwrap();
            let ready = Arc::new(AtomicBool::new(false));
            let observed = ready.clone();
            let output = std::thread::spawn(move || {
                let mut output = String::new();
                for line in BufReader::new(stdout).lines() {
                    let line = line.unwrap();
                    if line == "close-fixture-ready" {
                        observed.store(true, Ordering::Release);
                    }
                    output.push_str(&line);
                    output.push('\n');
                }
                output
            });
            Self {
                card: TestCard(Mutex::new(Some(child))),
                input,
                ready,
                output,
            }
        }
        fn finish(self) -> String {
            // The child has its own bounded deadline, including assertion failures.
            if let Some(child) = self.card.0.lock().unwrap().as_mut() {
                child.wait().unwrap();
            }
            self.output.join().unwrap()
        }
    }

    fn signal(message: &str) {
        println!("{message}");
        std::io::stdout().flush().unwrap();
    }

    #[test]
    fn close_fixture() {
        let Ok(mode) = std::env::var("LUMATAPE_TEST_CLOSE_FIXTURE") else {
            return;
        };
        let (send, receive) = mpsc::channel();
        std::thread::spawn(move || {
            for byte in std::io::stdin().bytes() {
                if send.send(byte.unwrap()).is_err() {
                    break;
                }
            }
        });
        assert_eq!(receive.recv_timeout(Duration::from_secs(3)).unwrap(), b's');
        std::thread::sleep(Duration::from_millis(150));
        if mode == "native" {
            #[cfg(windows)]
            assert!(delayed_native_window());
            #[cfg(not(windows))]
            panic!("native fixture requires Windows");
        } else {
            signal("close-fixture-ready");
            assert_eq!(receive.recv_timeout(Duration::from_secs(3)).unwrap(), b'c');
        }
        signal("close-fixture-closed");
    }

    #[test]
    fn close_retries_after_child_becomes_ready() {
        let mut fixture = Fixture::new("portable");
        let mut attempts = 0;
        let mut sent = false;
        let result =
            tauri::async_runtime::block_on(fixture.card.close_with(Duration::from_secs(2), |_| {
                attempts += 1;
                if attempts == 1 {
                    fixture.input.write_all(b"s").unwrap();
                } else if fixture.ready.load(Ordering::Acquire) && !sent {
                    fixture.input.write_all(b"c").unwrap();
                    sent = true;
                }
            }));
        let output = fixture.finish();
        assert!(result.is_ok(), "{result:?}\n{output}");
        assert!(
            attempts > 1 && sent && output.contains("close-fixture-closed"),
            "{output}"
        );
    }

    #[test]
    fn close_timeout_preserves_child_for_retry() {
        let mut fixture = Fixture::new("portable");
        let mut started = false;
        let first = tauri::async_runtime::block_on(fixture.card.close_with(
            Duration::from_millis(40),
            |_| {
                if !started {
                    fixture.input.write_all(b"s").unwrap();
                    started = true;
                }
            },
        ));
        assert!(first.is_err());
        assert!(fixture
            .card
            .0
            .lock()
            .unwrap()
            .as_mut()
            .unwrap()
            .try_wait()
            .unwrap()
            .is_none());
        let mut sent = false;
        let retry =
            tauri::async_runtime::block_on(fixture.card.close_with(Duration::from_secs(2), |_| {
                if fixture.ready.load(Ordering::Acquire) && !sent {
                    fixture.input.write_all(b"c").unwrap();
                    sent = true;
                }
            }));
        let output = fixture.finish();
        assert!(
            retry.is_ok() && sent && output.contains("close-fixture-closed"),
            "{retry:?}\n{output}"
        );
    }

    #[cfg(windows)]
    #[test]
    #[ignore = "requires an interactive Windows desktop; run windows-testcard-close-smoke.ps1"]
    fn delayed_native_hwnd_is_closed() {
        let mut fixture = Fixture::new("native");
        let mut attempts = 0;
        let result = tauri::async_runtime::block_on(fixture.card.close_with(
            Duration::from_secs(4),
            |pid| {
                native::windows_for(pid, true);
                attempts += 1;
                if attempts == 1 {
                    // The child cannot create its HWND until AFTER this first scan.
                    fixture.input.write_all(b"s").unwrap();
                }
            },
        ));
        let output = fixture.finish();
        assert!(
            result.is_ok() && attempts > 1 && output.contains("close-fixture-closed"),
            "{result:?}\n{output}"
        );
    }

    #[cfg(windows)]
    fn delayed_native_window() -> bool {
        use std::ffi::c_void;
        type Hwnd = *mut c_void;
        #[repr(C)]
        struct Message {
            hwnd: Hwnd,
            message: u32,
            wparam: usize,
            lparam: isize,
            time: u32,
            x: i32,
            y: i32,
            private: u32,
        }
        #[link(name = "user32")]
        extern "system" {
            fn CreateWindowExW(
                ex: u32,
                class: *const u16,
                title: *const u16,
                style: u32,
                x: i32,
                y: i32,
                width: i32,
                height: i32,
                parent: Hwnd,
                menu: Hwnd,
                instance: Hwnd,
                parameter: Hwnd,
            ) -> Hwnd;
            fn PeekMessageW(
                message: *mut Message,
                hwnd: Hwnd,
                min: u32,
                max: u32,
                remove: u32,
            ) -> i32;
            fn DispatchMessageW(message: *const Message) -> isize;
            fn IsWindow(hwnd: Hwnd) -> i32;
            fn DestroyWindow(hwnd: Hwnd) -> i32;
        }
        let class: Vec<u16> = "STATIC\0".encode_utf16().collect();
        unsafe {
            // Hidden, owned top-level HWND: no focus, desktop input or user profile changes.
            let hwnd = CreateWindowExW(
                0,
                class.as_ptr(),
                class.as_ptr(),
                0,
                0,
                0,
                64,
                64,
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
                std::ptr::null_mut(),
            );
            assert!(!hwnd.is_null());
            signal("close-fixture-ready");
            let deadline = std::time::Instant::now() + Duration::from_secs(3);
            while IsWindow(hwnd) != 0 && std::time::Instant::now() < deadline {
                let mut message: Message = std::mem::zeroed();
                while PeekMessageW(&mut message, std::ptr::null_mut(), 0, 0, 1) != 0 {
                    DispatchMessageW(&message);
                }
                std::thread::sleep(Duration::from_millis(5));
            }
            let closed = IsWindow(hwnd) == 0;
            if !closed {
                DestroyWindow(hwnd);
            }
            closed
        }
    }
}
