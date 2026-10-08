//! Physical-pixel placement; no OS handles or focus changes live here.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Bounds {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Layout {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
    pub min_width: u32,
    pub min_height: u32,
}

pub fn layout(work: Bounds, scale: f64, frame: (u32, u32), anchor: Option<Bounds>) -> Layout {
    let scale = if scale.is_finite() && scale > 0.0 {
        scale
    } else {
        1.0
    };
    let px = |dip: f64| (dip * scale).round().max(1.0) as u32;
    // Keep at least one pixel usable even on a work area smaller than our minimum.
    let margin_x = px(8.0).min(work.width.saturating_sub(frame.0 + 1) / 2);
    let margin_y = px(8.0).min(work.height.saturating_sub(frame.1 + 1) / 2);
    let available_width = work.width.saturating_sub(margin_x * 2 + frame.0).max(1);
    let available_height = work.height.saturating_sub(margin_y * 2 + frame.1).max(1);
    let width = px(440.0).min(available_width);
    let height = px(640.0).min(available_height);
    let outer_width = i64::from(width) + i64::from(frame.0);
    let outer_height = i64::from(height) + i64::from(frame.1);
    let left = i64::from(work.x);
    let top = i64::from(work.y);
    let right = left + i64::from(work.width);
    let bottom = top + i64::from(work.height);
    let (x, y) = match anchor {
        Some(icon) => {
            let center_x = i64::from(icon.x) + i64::from(icon.width) / 2;
            let center_y = i64::from(icon.y) + i64::from(icon.height) / 2;
            let gap = i64::from(px(8.0));
            // Works for all taskbar edges and for Shell's overflow icon rectangle.
            let distances = [
                (center_y - bottom).abs(),
                (center_y - top).abs(),
                (center_x - left).abs(),
                (center_x - right).abs(),
            ];
            let edge = distances
                .iter()
                .enumerate()
                .min_by_key(|(_, distance)| *distance)
                .map(|(edge, _)| edge)
                .unwrap();
            match edge {
                0 => (
                    center_x - outer_width / 2,
                    i64::from(icon.y) - gap - outer_height,
                ),
                1 => (
                    center_x - outer_width / 2,
                    i64::from(icon.y) + i64::from(icon.height) + gap,
                ),
                2 => (
                    i64::from(icon.x) + i64::from(icon.width) + gap,
                    center_y - outer_height / 2,
                ),
                _ => (
                    i64::from(icon.x) - gap - outer_width,
                    center_y - outer_height / 2,
                ),
            }
        }
        None => (
            left + (i64::from(work.width) - outer_width) / 2,
            top + (i64::from(work.height) - outer_height) / 2,
        ),
    };
    let min_x = left + i64::from(margin_x);
    let min_y = top + i64::from(margin_y);
    Layout {
        x: x.clamp(
            min_x,
            (right - i64::from(margin_x) - outer_width).max(min_x),
        ) as i32,
        y: y.clamp(
            min_y,
            (bottom - i64::from(margin_y) - outer_height).max(min_y),
        ) as i32,
        width,
        height,
        min_width: px(360.0).min(available_width),
        min_height: px(480.0).min(available_height),
    }
}

/// Hiding without a reachable tray would strand the only settings window.
pub fn hide_if_reachable<E>(
    tray_available: bool,
    hide: impl FnOnce() -> Result<(), E>,
) -> Result<bool, E> {
    if !tray_available {
        return Ok(false);
    }
    hide()?;
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;
    fn bounds(x: i32, y: i32, width: u32, height: u32) -> Bounds {
        Bounds {
            x,
            y,
            width,
            height,
        }
    }
    fn contained(result: Layout, work: Bounds, frame: (u32, u32)) {
        assert!(result.x >= work.x && result.y >= work.y);
        assert!(
            i64::from(result.x) + i64::from(result.width + frame.0)
                <= i64::from(work.x) + i64::from(work.width)
        );
        assert!(
            i64::from(result.y) + i64::from(result.height + frame.1)
                <= i64::from(work.y) + i64::from(work.height)
        );
        assert!(result.min_width <= result.width && result.min_height <= result.height);
    }
    #[test]
    fn bottom_taskbar_on_negative_monitor_at_150_percent() {
        let work = bounds(-2560, -200, 2560, 1380);
        let placed = layout(work, 1.5, (0, 0), Some(bounds(-90, 1190, 32, 32)));
        assert_eq!(
            (
                placed.width,
                placed.height,
                placed.min_width,
                placed.min_height
            ),
            (660, 960, 540, 720)
        );
        assert_eq!(placed.x, -672);
        assert_eq!(placed.y, 208);
        contained(placed, work, (0, 0));
    }
    #[test]
    fn all_taskbar_edges_and_overflow_stay_in_target_work_area() {
        let work = bounds(1600, -1080, 1920, 1040);
        for icon in [
            bounds(3000, -30, 24, 24),
            bounds(1700, -1110, 24, 24),
            bounds(1570, -600, 24, 24),
            bounds(3530, -600, 24, 24),
            bounds(3400, -180, 24, 24),
        ] {
            contained(layout(work, 1.0, (0, 0), Some(icon)), work, (0, 0));
        }
    }
    #[test]
    fn high_dpi_small_work_area_reduces_minimum_and_accounts_for_frame() {
        let work = bounds(0, 0, 1280, 680);
        let placed = layout(work, 2.0, (16, 38), Some(bounds(1200, 690, 30, 30)));
        assert_eq!((placed.width, placed.height), (880, 610));
        assert_eq!((placed.min_width, placed.min_height), (720, 610));
        contained(placed, work, (16, 38));
    }
    #[test]
    fn first_launch_is_centered_and_invalid_scale_is_bounded() {
        for scale in [1.0, f64::NAN, 0.0, -1.0] {
            let placed = layout(bounds(100, 50, 1000, 800), scale, (0, 0), None);
            assert_eq!(
                (placed.x, placed.y, placed.width, placed.height),
                (380, 130, 440, 640)
            );
        }
        contained(
            layout(bounds(0, 0, 200, 250), 3.0, (0, 0), None),
            bounds(0, 0, 200, 250),
            (0, 0),
        );
    }
    #[test]
    fn missing_tray_never_hides_and_a_failed_hide_never_reports_hidden() {
        assert_eq!(
            hide_if_reachable::<()>(false, || panic!("must remain reachable")),
            Ok(false)
        );
        assert_eq!(
            hide_if_reachable(true, || Err("window error")),
            Err("window error")
        );
        let mut called = false;
        assert_eq!(
            hide_if_reachable::<()>(true, || {
                called = true;
                Ok(())
            }),
            Ok(true)
        );
        assert!(called);
    }
}
