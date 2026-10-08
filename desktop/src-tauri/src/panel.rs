use crate::panel_geometry::{self, Bounds};
use tauri::{Manager, PhysicalPosition, PhysicalSize, WebviewWindow};

/// The tray API returns physical pixels. Select its monitor before applying DPI.
pub fn fit(window: &WebviewWindow, anchor: Option<tauri::Rect>) {
    let anchor = anchor.map(|rect| {
        let position = rect.position.to_physical::<i32>(1.0);
        let size = rect.size.to_physical::<u32>(1.0);
        Bounds {
            x: position.x,
            y: position.y,
            width: size.width,
            height: size.height,
        }
    });
    let monitor = anchor
        .and_then(|icon| {
            window
                .monitor_from_point(
                    f64::from(icon.x) + f64::from(icon.width) / 2.0,
                    f64::from(icon.y) + f64::from(icon.height) / 2.0,
                )
                .ok()
                .flatten()
        })
        .or_else(|| window.current_monitor().ok().flatten());
    let Some(monitor) = monitor else { return };
    let (Ok(inner), Ok(outer)) = (window.inner_size(), window.outer_size()) else {
        return;
    };
    let work = monitor.work_area();
    let placed = panel_geometry::layout(
        Bounds {
            x: work.position.x,
            y: work.position.y,
            width: work.size.width,
            height: work.size.height,
        },
        monitor.scale_factor(),
        (
            outer.width.saturating_sub(inner.width),
            outer.height.saturating_sub(inner.height),
        ),
        anchor,
    );
    let _ = window.set_min_size(Some(PhysicalSize::new(placed.min_width, placed.min_height)));
    let size = PhysicalSize::new(placed.width, placed.height);
    if inner != size {
        let _ = window.set_size(size);
    }
    let position = PhysicalPosition::new(placed.x, placed.y);
    if window.outer_position().ok() != Some(position) {
        let _ = window.set_position(position);
    }
}

pub fn show(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        fit(&window, crate::tray::anchor(app));
        let _ = window.show();
        let _ = window.unminimize();
        // Only explicit tray/settings/single-instance requests reach this path.
        let _ = window.set_focus();
        crate::ui_state(&window, true);
    }
}

pub fn hide(window: &WebviewWindow) -> Result<bool, String> {
    let reachable = crate::tray::can_hide(window.app_handle());
    let hidden = panel_geometry::hide_if_reachable(reachable, || window.hide())
        .map_err(|error| error.to_string())?;
    if hidden {
        crate::ui_state(window, false);
    }
    Ok(hidden)
}

pub fn toggle(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        if window.is_visible().unwrap_or(false) && !window.is_minimized().unwrap_or(false) {
            if let Err(error) = hide(&window) {
                eprintln!("LumaTape panel hide: {error}");
            }
        } else {
            show(app);
        }
    }
}
