//! Host ownership of an unconfirmed display change, independent of UI snapshots.
use std::sync::atomic::{AtomicU64, Ordering};

// A file picker, update dialog or another ordinary action must not occupy the
// command slot while the user has only a few seconds to keep/revert a mode.
pub fn allowed_while_pending(id: &str) -> bool {
    matches!(id, "confirm" | "model:format:off" | "model:off" | "quit")
}

#[derive(Default)]
pub struct DisplayChange {
    generation: AtomicU64,
    active: AtomicU64,
}
impl DisplayChange {
    pub fn begin(&self) -> u64 {
        let ticket = self.generation.fetch_add(1, Ordering::AcqRel) + 1;
        self.active.store(ticket, Ordering::Release);
        ticket
    }
    pub fn ticket(&self) -> u64 {
        self.active.load(Ordering::Acquire)
    }
    pub fn pending(&self) -> bool {
        self.ticket() != 0
    }
    pub fn clear(&self, ticket: u64) {
        if ticket != 0 {
            let _ = self
                .active
                .compare_exchange(ticket, 0, Ordering::AcqRel, Ordering::Acquire);
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn late_snapshot_or_confirmation_cannot_clear_a_new_display_change() {
        let state = DisplayChange::default();
        let old = state.begin();
        state.clear(old);
        let new = state.begin();
        assert_ne!(new, old);
        state.clear(old);
        assert!(state.pending());
        assert_eq!(state.ticket(), new);
        state.clear(new);
        assert!(!state.pending());
    }

    #[test]
    fn pending_display_admits_only_resolution_decisions_or_emergency_exit() {
        for id in ["confirm", "model:format:off", "model:off", "quit"] {
            assert!(allowed_while_pending(id), "{id}");
        }
        for id in [
            "import",
            "testcard",
            "diagnostics",
            "hotkey-test",
            "updates",
            "autostart",
            "about",
            "effects-folder",
            "details",
            "model:power",
            "model:format:system",
            "model:format:window",
            "model:preset:VHS Tape",
            "unknown",
        ] {
            assert!(!allowed_while_pending(id), "{id}");
        }
    }
}
