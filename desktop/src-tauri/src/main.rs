#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod autostart;
mod engine;
mod i18n;
mod lifecycle;
mod links;
mod native_dialog;
mod protocol;
mod testcard;
mod tray;
mod tray_model;
mod updater;
use engine::Engine;
use protocol::Request;
use serde_json::Value;
use std::sync::{
    atomic::{AtomicBool, AtomicU8, Ordering},
    Arc,
};
use tauri::{Listener, Manager};

struct Host {
    engine: Result<Arc<Engine>, String>,
    engine_directory: std::path::PathBuf,
    testcard: testcard::TestCard,
    updates: updater::Updater,
    autostart: autostart::Controller,
    terminal: AtomicU8,
    allow_exit: AtomicBool,
    tray: tray::Controller,
}
impl Host {
    fn engine(&self) -> Result<&Arc<Engine>, String> {
        self.engine.as_ref().map_err(Clone::clone)
    }
}
// All native commands retain the same validation and terminal ownership boundary.
async fn request_engine(app: &tauri::AppHandle, request: Request) -> Result<Value, String> {
    let state = app.state::<Host>();
    if state.terminal.load(Ordering::Acquire) != 0
        && !protocol::allowed_during_terminal(&request.kind)
    {
        return Err(crate::i18n::text(
            "LumaTape закрывается или обновляется.",
            "LumaTape is closing or updating.",
        )
        .into());
    }
    let mutates = protocol::mutates(&request.kind);
    if mutates {
        state.tray.invalidate();
    }
    let result = state.engine()?.request(request).await;
    if mutates {
        state.tray.invalidate();
    }
    result
}
// Terminal ownership rejects new startup changes. Drain an already running
// registry operation before stopping the host or handing over to NSIS.
async fn finish_autostart_change(app: &tauri::AppHandle) -> Result<(), String> {
    let target = app.clone();
    tauri::async_runtime::spawn_blocking(move || target.state::<Host>().autostart.wait_idle())
        .await
        .map_err(|error| error.to_string())
}

async fn quit_host(app: &tauri::AppHandle) -> Result<(), String> {
    let state = app.state::<Host>();
    // Quit has already cancelled a pending update through the tray. Give its
    // bounded network operation time to unwind before taking shutdown ownership.
    let wait_until = std::time::Instant::now() + std::time::Duration::from_secs(3);
    while state.terminal.load(Ordering::Acquire) == 4 && std::time::Instant::now() < wait_until {
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;
    }
    if state
        .terminal
        .compare_exchange(0, 1, Ordering::AcqRel, Ordering::Acquire)
        .is_err()
    {
        return Err(crate::i18n::text(
            "LumaTape уже закрывается или обновляется.",
            "LumaTape is already closing or updating.",
        )
        .into());
    }
    if let Err(error) = finish_autostart_change(app).await {
        let _ = state
            .terminal
            .compare_exchange(1, 0, Ordering::AcqRel, Ordering::Acquire);
        return Err(error);
    }
    // Keep the engine usable if its own test scene cannot close. Terminal
    // ownership already blocks new scene launches, as in the updater path.
    let result = match state.testcard.close().await {
        Ok(()) => match state.engine() {
            Ok(engine) => engine.stop_for_explicit_quit().await,
            Err(_) => Ok(1), // Start failed: no engine exists, but retain a failure exit.
        },
        Err(error) => Err(error),
    };
    match result {
        Ok(exit_code) => {
            state.allow_exit.store(true, Ordering::Release);
            app.exit(exit_code);
            Ok(())
        }
        Err(error) => {
            let _ = state
                .terminal
                .compare_exchange(1, 0, Ordering::AcqRel, Ordering::Acquire);
            Err(error)
        }
    }
}

async fn install_update_host(
    app: &tauri::AppHandle,
    version: &str,
    epoch: u64,
) -> Result<updater::Status, String> {
    let state = app.state::<Host>();
    if state.tray.cancel_epoch().load(Ordering::Acquire) != epoch {
        return Err(crate::i18n::text(
            "Обновление отменено. Можно проверить ещё раз.",
            "Update cancelled. You can check again.",
        )
        .into());
    }
    if state
        .terminal
        .compare_exchange(0, 2, Ordering::AcqRel, Ordering::Acquire)
        .is_err()
    {
        return Err(crate::i18n::text(
            "LumaTape уже закрывается или обновляется.",
            "LumaTape is already closing or updating.",
        )
        .into());
    }
    if let Err(error) = finish_autostart_change(app).await {
        let _ = state
            .terminal
            .compare_exchange(2, 0, Ordering::AcqRel, Ordering::Acquire);
        return Err(error);
    }
    let result = match state.engine() {
        Ok(engine) => {
            state
                .updates
                .install(
                    app,
                    version,
                    engine,
                    &state.testcard,
                    &state.terminal,
                    state.tray.cancel_epoch(),
                    epoch,
                )
                .await
        }
        Err(error) => Err(error),
    };
    if result
        .as_ref()
        .is_ok_and(|status| status.state == "installing")
    {
        // The checked installer owns continuation now. Keep terminal ownership
        // until exit so no queued tray action can restart the stopped engine.
        state.allow_exit.store(true, Ordering::Release);
        app.exit(0);
    } else {
        for owned in [2, 3, 4] {
            let _ = state
                .terminal
                .compare_exchange(owned, 0, Ordering::AcqRel, Ordering::Acquire);
        }
    }
    result
}
fn engine_clean_exit(app: &tauri::AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let Some(state) = app.try_state::<Host>() else {
            return;
        };
        state.terminal.store(1, Ordering::Release);
        let closed = match finish_autostart_change(&app).await {
            Ok(()) => state.testcard.close().await,
            Err(error) => Err(error),
        };
        match closed {
            Ok(()) => {
                state.allow_exit.store(true, Ordering::Release);
                app.exit(0);
            }
            Err(error) => {
                let _ = state
                    .terminal
                    .compare_exchange(1, 0, Ordering::AcqRel, Ordering::Acquire);
                tray::report_error(
                    &app,
                    crate::i18n::text("Завершение", "Shutting down"),
                    &error,
                );
            }
        }
    });
}
fn main() {
    let builder =
        tauri::Builder::default().plugin(tauri_plugin_single_instance::init(|app, _, _| {
            tray::show_existing(app);
        }));
    #[cfg(windows)]
    let builder = builder
        .plugin(tauri_plugin_opener::init())
        .plugin(autostart::plugin())
        .plugin(
            tauri_plugin_updater::Builder::new()
                .pubkey(updater::PUBLIC_KEY.unwrap_or(""))
                .build(),
        );
    let app = builder
        .setup(|app| {
            let root = app.path().resource_dir()?.join("engine");
            let engine = Engine::start(app.handle().clone(), &root);
            app.manage(Host {
                engine,
                engine_directory: root,
                testcard: testcard::TestCard::new(),
                updates: updater::Updater::new(),
                autostart: autostart::Controller::new(),
                terminal: AtomicU8::new(0),
                allow_exit: AtomicBool::new(false),
                tray: tray::Controller::new(),
            });
            let update_app = app.handle().clone();
            app.listen("updater_state", move |_| tray::updater_event(&update_app));
            tray::install(app.handle());
            if app.state::<Host>().updates.configured() {
                let app = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    tokio::time::sleep(std::time::Duration::from_secs(20)).await;
                    let state = app.state::<Host>();
                    if state.terminal.load(Ordering::Acquire) == 0
                        && state.updates.status().state == "idle"
                    {
                        // A failed background check stays in the Updates menu;
                        // never interrupt a game with an unsolicited dialog.
                        let _ = state.updates.check(&app).await;
                    }
                });
            }
            Ok(())
        })
        .build(tauri::generate_context!());
    let app = match app {
        Ok(app) => app,
        Err(error) => {
            let _ = native_dialog::show_error(
                "LumaTape",
                &crate::localized!(
                    "Не удалось запустить LumaTape.\n\n{error}",
                    "Could not start LumaTape.\n\n{error}"
                ),
            );
            std::process::exit(1);
        }
    };
    app.run(|app, event| {
        if let tauri::RunEvent::ExitRequested { api, .. } = event {
            if !app.state::<Host>().allow_exit.load(Ordering::Acquire) {
                api.prevent_exit();
            }
        }
    });
}
