use crate::protocol::{self, Request};
use serde_json::{json, Value};
use std::{
    collections::HashMap,
    io::{BufReader, Write},
    path::Path,
    process::{Command, Stdio},
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        mpsc::{sync_channel, SyncSender},
        Arc, Mutex,
    },
    time::Duration,
};
use tauri::{AppHandle, Emitter};
use tokio::sync::oneshot;

type Reply = oneshot::Sender<Result<Value, String>>;
pub struct Engine {
    writer: Mutex<Option<SyncSender<Vec<u8>>>>,
    pending: Mutex<HashMap<String, Reply>>,
    next: AtomicU64,
    stopping: AtomicBool,
    exited: Mutex<Option<Result<i32, String>>>,
    output_done: AtomicBool,
    clean_stopped: AtomicBool,
    finalized: AtomicBool,
    terminal_errors: protocol::TerminalErrors,
}
impl Engine {
    pub fn start(app: AppHandle, directory: &Path) -> Result<Arc<Self>, String> {
        #[cfg(not(windows))]
        {
            let _ = (app, directory);
            return Err(crate::i18n::text(
                "Движок захвата доступен только в Windows",
                "The capture engine requires Windows",
            )
            .into());
        }
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            // The frontend never supplies a path, executable or command-line argument.
            for name in [
                "lumatape-engine.exe",
                "glfw3.dll",
                "lumatape_capture.dll",
                "lumatape-watchdog.exe",
            ] {
                if !directory.join(name).is_file() {
                    return Err(crate::localized!(
                        "В комплекте отсутствует компонент движка: {name}",
                        "Bundled engine component missing: {name}"
                    ));
                }
            }
            let mut child = Command::new(directory.join("lumatape-engine.exe"))
                .args([
                    "--control-stdio",
                    "--controller-pid",
                    &std::process::id().to_string(),
                    "--headless-settings",
                ])
                .env("LUMATAPE_UI_LANGUAGE", crate::i18n::language().code())
                .current_dir(directory)
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .creation_flags(0x08000000)
                .spawn()
                .map_err(|e| {
                    crate::localized!(
                        "Не удалось запустить движок: {e}",
                        "Engine start failed: {e}"
                    )
                })?;
            let mut stdin = child.stdin.take().ok_or(crate::i18n::text(
                "Недоступен канал ввода движка",
                "Missing engine stdin",
            ))?;
            let stdout = child.stdout.take().ok_or(crate::i18n::text(
                "Недоступен канал ответа движка",
                "Missing engine stdout",
            ))?;
            let stderr = child.stderr.take().ok_or(crate::i18n::text(
                "Недоступен канал ошибок движка",
                "Missing engine stderr",
            ))?;
            let (tx, rx) = sync_channel::<Vec<u8>>(protocol::MAX_PENDING + 2);
            let engine = Arc::new(Self {
                writer: Mutex::new(Some(tx)),
                pending: Mutex::new(HashMap::new()),
                next: AtomicU64::new(1),
                stopping: AtomicBool::new(false),
                exited: Mutex::new(None),
                output_done: AtomicBool::new(false),
                clean_stopped: AtomicBool::new(false),
                finalized: AtomicBool::new(false),
                terminal_errors: protocol::TerminalErrors::default(),
            });
            let owner = engine.clone();
            std::thread::spawn(move || {
                for line in rx {
                    if let Err(e) = stdin.write_all(&line).and_then(|_| stdin.flush()) {
                        owner.terminal_errors.fallback(
                            "engine_input_closed",
                            &crate::localized!(
                                "Канал ввода движка закрыт: {e}",
                                "Engine input closed: {e}"
                            ),
                        );
                        // Closing stdin requests cleanup. Let stdout drain any
                        // final reply; failing pending here can race the quit ACK.
                        owner.close_input();
                        break;
                    }
                }
            });
            let owner = engine.clone();
            let target = app.clone();
            std::thread::spawn(move || {
                let mut reader = BufReader::new(stdout);
                loop {
                    match protocol::read_line(&mut reader) {
                        Ok(Some(line)) => {
                            let parsed: Result<Value, _> = serde_json::from_slice(&line);
                            match parsed {
                                Ok(value) if protocol::validate_response(&value).is_ok() => {
                                    owner.receive(&target, value)
                                }
                                _ => {
                                    owner.terminal_errors.fallback(
                                        "engine_protocol_error",
                                        crate::i18n::text(
                                            "Движок вернул некорректный ответ протокола",
                                            "Invalid engine protocol output",
                                        ),
                                    );
                                    owner.fail_pending(crate::i18n::text(
                                        "Движок вернул некорректный ответ протокола",
                                        "Invalid engine protocol output",
                                    ));
                                    owner.close_input();
                                    break;
                                }
                            }
                        }
                        Ok(None) => {
                            owner.fail_pending(crate::i18n::text(
                                "Канал ответа движка закрыт",
                                "Engine output closed",
                            ));
                            break;
                        }
                        Err(e) => {
                            owner.terminal_errors.fallback("engine_protocol_error", &e);
                            owner.fail_pending(&e);
                            owner.close_input();
                            break;
                        }
                    }
                }
                owner.output_done.store(true, Ordering::Release);
                owner.finish_exit(&target);
            });
            // Drain stderr independently so diagnostics cannot deadlock the IPC stream.
            std::thread::spawn(move || {
                let mut reader = BufReader::new(stderr);
                while let Ok(Some(line)) = protocol::read_line(&mut reader) {
                    eprintln!("engine: {}", String::from_utf8_lossy(&line));
                }
            });
            let owner = engine.clone();
            std::thread::spawn(move || {
                let status = child
                    .wait()
                    .map(|s| s.code().unwrap_or(-1))
                    .map_err(|e| e.to_string());
                *owner.exited.lock().unwrap() = Some(status.clone());
                // Only stdout EOF may fail pending replies: wait() can win the race
                // against the reader draining the final cleanup acknowledgement.
                owner.finish_exit(&app);
            });
            Ok(engine)
        }
    }
    fn finish_exit(&self, app: &AppHandle) {
        if !self.output_done.load(Ordering::Acquire) {
            return;
        }
        let Some(status) = self.exited.lock().unwrap().clone() else {
            return;
        };
        if self.finalized.swap(true, Ordering::AcqRel) {
            return;
        }
        if !self.stopping.load(Ordering::Acquire) {
            if status == Ok(0) && self.clean_stopped.load(Ordering::Acquire) {
                crate::engine_clean_exit(app);
            } else {
                self.terminal_errors.fallback(
                    "engine_exited",
                    &crate::localized!(
                        "Движок завершился без подтверждения восстановления (код {:?})",
                        "Engine exited without confirmed cleanup (exit {:?})",
                        status.ok()
                    ),
                );
                let event = json!({"v":1,"event":"fatal","data":self.terminal_errors.current()});
                crate::tray::engine_event(app, &event);
                let _ = app.emit("engine_event", event);
            }
        }
    }
    fn fail_pending(&self, error: &str) {
        let error = self.terminal_errors.rejection(error);
        for (_, sender) in self.pending.lock().unwrap().drain() {
            let _ = sender.send(Err(error.clone()));
        }
    }
    fn close_input(&self) {
        self.writer.lock().unwrap().take();
    }
    fn receive(&self, app: &AppHandle, mut value: Value) {
        if let Some(id) = value.get("id").and_then(Value::as_str) {
            if let Some(sender) = self.pending.lock().unwrap().remove(id) {
                let _ = sender.send(protocol::response_result(value));
            }
            return;
        }
        if self.terminal_errors.record_event(&value) {
            value["data"] =
                serde_json::to_value(self.terminal_errors.current()).unwrap_or(Value::Null);
        }
        if value["event"] == "stopped" {
            self.clean_stopped
                .store(value["data"]["clean_shutdown"] == true, Ordering::Release);
        }
        crate::tray::engine_event(app, &value);
        // The engine emits this legacy panel hint during a normal disabled
        // startup too. Tray-only launch stays silent; state/errors appear in
        // the menu. Only a genuine second launch calls show_existing.
        let _ = app.emit("engine_event", value);
    }
    pub async fn request(&self, request: Request) -> Result<Value, String> {
        protocol::validate(&request)?;
        if self.stopping.load(Ordering::Acquire) && self.exited.lock().unwrap().is_none() {
            return Err(self.terminal_errors.rejection(crate::i18n::text(
                "Движок завершает работу",
                "Engine is stopping",
            )));
        }
        self.internal(request, Duration::from_secs(12)).await
    }
    pub async fn internal(&self, request: Request, timeout: Duration) -> Result<Value, String> {
        let process_finished = self.exited.lock().unwrap().is_some();
        if process_finished {
            // wait() can precede the stdout reader. Give its final fatal/ACK a
            // bounded opportunity to arrive before answering a new UI request.
            let deadline = tokio::time::Instant::now() + Duration::from_secs(1);
            while !self.output_done.load(Ordering::Acquire) {
                if tokio::time::Instant::now() >= deadline {
                    return Err(crate::i18n::text(
                        "Движок завершает передачу ответа; обновите состояние",
                        "Engine output is still finalizing; refresh its status",
                    )
                    .into());
                }
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
            return Err(self.terminal_errors.rejection(crate::i18n::text(
                "Движок не запущен",
                "Engine is not running",
            )));
        }
        let id = self.next.fetch_add(1, Ordering::Relaxed).to_string();
        let (send, receive) = oneshot::channel();
        {
            let mut pending = self.pending.lock().unwrap();
            // Preserve room for terminal/emergency requests when normal work is full.
            let limit = protocol::pending_limit(&request.kind);
            if pending.len() >= limit {
                return Err(crate::i18n::text(
                    "Очередь запросов движка заполнена",
                    "Engine request queue is full",
                )
                .into());
            }
            pending.insert(id.clone(), send);
        }
        let mut bytes = serde_json::to_vec(
            &json!({"v":1,"id":id,"type":request.kind,"payload":request.payload}),
        )
        .map_err(|e| e.to_string())?;
        bytes.push(b'\n');
        let queued = self
            .writer
            .lock()
            .unwrap()
            .as_ref()
            .ok_or_else(|| {
                crate::i18n::text("Канал ввода движка закрыт", "Engine input closed").to_string()
            })
            .and_then(|writer| {
                writer.try_send(bytes).map_err(|_| {
                    crate::i18n::text(
                        "Очередь запросов движка заполнена или закрыта",
                        "Engine request queue is full or closed",
                    )
                    .to_string()
                })
            });
        if let Err(e) = queued {
            self.pending.lock().unwrap().remove(&id);
            return Err(self.terminal_errors.rejection(&e));
        }
        let result = tokio::time::timeout(timeout, receive).await;
        self.pending.lock().unwrap().remove(&id);
        match result {
            Ok(Ok(reply)) => reply,
            Ok(Err(_)) => Err("Engine response channel closed".into()),
            Err(_) => Err("Engine response timed out; operation outcome must be refreshed".into()),
        }
    }
    pub async fn stop_for_explicit_quit(&self) -> Result<i32, String> {
        let cleanup = self.stop().await;
        // A failed wait is not proof of process termination. Only an OS exit
        // status permits dismissing a failed engine on an explicit user quit.
        let confirmed_exit = self
            .exited
            .lock()
            .unwrap()
            .as_ref()
            .and_then(|r| r.as_ref().ok())
            .copied();
        if cleanup.is_err() && confirmed_exit.is_some() {
            eprintln!(
                "Engine had already exited; closing host with failure: {}",
                cleanup.as_ref().unwrap_err()
            );
        }
        crate::lifecycle::explicit_quit_exit_code(cleanup, confirmed_exit)
    }
    pub async fn stop(&self) -> Result<(), String> {
        if let Some(status) = self.exited.lock().unwrap().clone() {
            return status.and_then(|code| {
                if code == 0 && self.clean_stopped.load(Ordering::Acquire) {
                    Ok(())
                } else {
                    Err(crate::localized!(
                        "Движок завершился с кодом {code}",
                        "Engine exited with code {code}"
                    ))
                }
            });
        }
        if self.stopping.swap(true, Ordering::AcqRel) {
            return Err(crate::i18n::text(
                "Движок уже завершает работу",
                "Engine shutdown already in progress",
            )
            .into());
        }
        let reply = self
            .internal(
                Request {
                    kind: "quit".into(),
                    payload: Value::Null,
                },
                Duration::from_secs(8),
            )
            .await;
        self.close_input(); // EOF independently requests cleanup when the RPC failed.
        let deadline = tokio::time::Instant::now() + Duration::from_secs(8);
        loop {
            let exited = self.exited.lock().unwrap().clone();
            if let Some(status) = exited {
                let acknowledgement = reply?;
                if acknowledgement["clean_shutdown"] != true {
                    return Err(crate::i18n::text(
                        "Движок не подтвердил корректное завершение",
                        "Engine did not acknowledge clean shutdown",
                    )
                    .into());
                }
                return status.and_then(|code| {
                    if code == 0 {
                        Ok(())
                    } else {
                        Err(crate::localized!(
                            "Восстановление движка завершилось с кодом {code}",
                            "Engine cleanup exited with code {code}"
                        ))
                    }
                });
            }
            if tokio::time::Instant::now() >= deadline {
                return Err(crate::i18n::text(
                    "Движок не подтвердил завершение; установка или выход отменены",
                    "Engine did not confirm shutdown; installation/exit cancelled",
                )
                .into());
            }
            tokio::time::sleep(Duration::from_millis(25)).await;
        }
    }
}
