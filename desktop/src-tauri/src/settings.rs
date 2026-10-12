//! Lazy local settings window. No general-purpose engine, shell or filesystem IPC.
use crate::{i18n::text, tray, Host};
use serde_json::Value;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};

fn local_window(window: &WebviewWindow) -> Result<(), String> {
    if window.label() == "main" {
        Ok(())
    } else {
        Err("Unknown settings window".into())
    }
}
fn allowed_action(id: &str) -> bool {
    crate::tray_model::parse_action(id).is_some()
        || matches!(
            id,
            "confirm"
                | "import"
                | "diagnostics"
                | "testcard"
                | "hotkey-test"
                | "updates"
                | "autostart"
                | "about"
                | "effects-folder"
        )
}

#[tauri::command]
pub async fn settings_snapshot(window: WebviewWindow, catalogues: bool) -> Result<Value, String> {
    local_window(&window)?;
    tray::settings_data(window.app_handle(), catalogues).await
}

#[tauri::command]
pub async fn settings_action(
    window: WebviewWindow,
    id: String,
    sequence: u64,
    intent_epoch: u64,
    confirmation_token: Option<String>,
) -> Result<(), String> {
    local_window(&window)?;
    if !allowed_action(&id) {
        return Err(text("Неизвестная команда.", "Unknown command.").into());
    }
    if id == "confirm" && confirmation_token.is_none() {
        return Err(text(
            "Нет ожидающего видеорежима.",
            "No display change is pending.",
        )
        .into());
    }
    tray::execute(
        window.app_handle(),
        &id,
        Some(sequence),
        confirmation_token.as_deref(),
        Some(intent_epoch),
    )
    .await
}

pub async fn open(app: &AppHandle) -> Result<(), String> {
    let (send, receive) = tokio::sync::oneshot::channel();
    let target = app.clone();
    // Serialize construction on the event loop, including concurrent second launches.
    app.run_on_main_thread(move || {
        let _ = send.send(open_on_main(&target));
    })
    .map_err(|e| e.to_string())?;
    receive.await.map_err(|e| e.to_string())?
}
fn open_on_main(app: &AppHandle) -> Result<(), String> {
    if app.state::<Host>().terminal.load(Ordering::Acquire) != 0 {
        return Err(text(
            "LumaTape закрывается или обновляется.",
            "LumaTape is closing or updating.",
        )
        .into());
    }
    if let Some(window) = app.get_webview_window("main") {
        window.unminimize().map_err(|e| e.to_string())?;
        window.show().map_err(|e| e.to_string())?;
        return window.set_focus().map_err(|e| e.to_string());
    }
    let window = WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
        .title(text("LumaTape — Настройки", "LumaTape — Settings"))
        .inner_size(620.0, 740.0)
        .min_inner_size(400.0, 480.0)
        .center()
        .on_navigation(|url| url.scheme() == "tauri" || url.host_str() == Some("tauri.localhost"))
        .build()
        .map_err(|e| e.to_string())?;
    let handle = app.clone();
    let closing = Arc::new(AtomicBool::new(false));
    window.on_window_event(move |event| {
        if let WindowEvent::CloseRequested { api, .. } = event {
            api.prevent_close();
            if closing.swap(true, Ordering::AcqRel) {
                return;
            }
            let app = handle.clone();
            let closing = closing.clone();
            tauri::async_runtime::spawn(async move {
                // Closing during an unconfirmed switch is cancellation, never acceptance.
                // Off crosses the emergency barrier even if apply is still in flight.
                let state = app.state::<Host>();
                tray::cancel_pending_actions(&state);
                let ticket = state.tray.display_change.ticket();
                let pending = state.tray.display_change.pending();
                let result = if pending {
                    tray::execute(&app, "model:off", None, None, None).await
                } else {
                    Ok(())
                };
                match result {
                    Ok(()) => {
                        app.state::<Host>().tray.display_change.clear(ticket);
                        if let Some(window) = app.get_webview_window("main") {
                            let _ = window.destroy();
                        }
                    }
                    Err(error) => tray::report_error(
                        &app,
                        text("Восстановление не завершено", "Restoration incomplete"),
                        &error,
                    ),
                }
                closing.store(false, Ordering::Release);
            });
        }
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn settings_does_not_expose_generic_ipc_or_process_commands() {
        for id in [
            "apply",
            "reload",
            "ui_state",
            "quit",
            "shell",
            "open_file",
            "shader_import",
            "unknown",
            "model:source:zz",
        ] {
            assert!(!allowed_action(id), "{id}");
        }
        for id in [
            "model:off",
            "model:power",
            "model:format:window",
            "model:format:off",
            "confirm",
            "diagnostics",
            "import",
        ] {
            assert!(allowed_action(id), "{id}");
        }
    }
}
