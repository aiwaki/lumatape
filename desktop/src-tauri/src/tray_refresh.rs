//! Order the fast status reads and slower catalogue reads before publishing UI.
use std::sync::atomic::{AtomicU64, Ordering};
use tokio::sync::Notify;

#[derive(Clone, Copy)]
pub struct Ticket {
    generation: u64,
    order: u64,
}

#[derive(Default)]
pub struct RefreshState {
    generation: AtomicU64,
    next: AtomicU64,
    applied: AtomicU64,
    wake: Notify,
}

impl RefreshState {
    pub fn invalidate(&self) {
        self.generation.fetch_add(1, Ordering::AcqRel);
        // A permit survives an in-flight read. Repeated events coalesce rather
        // than spawning more requests or losing the final state transition.
        self.wake.notify_one();
    }

    pub async fn changed(&self) {
        self.wake.notified().await;
    }

    pub fn begin(&self) -> Ticket {
        Ticket {
            generation: self.generation.load(Ordering::Acquire),
            order: self.next.fetch_add(1, Ordering::AcqRel) + 1,
        }
    }

    /// Call on the UI thread, for both success and failure. A late catalogue
    /// failure must not erase a newer successful status-only read.
    pub fn accept(&self, ticket: Ticket) -> bool {
        if ticket.generation != self.generation.load(Ordering::Acquire) {
            // At startup the event worker may have consumed the notification
            // while the only full refresh was still running and no cache
            // existed yet. Re-arm it when that obsolete result reaches the UI.
            self.wake.notify_one();
            return false;
        }
        let previous = self.applied.load(Ordering::Acquire);
        if ticket.order <= previous {
            return false;
        }
        self.applied.store(ticket.order, Ordering::Release);
        true
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[test]
    fn newer_status_survives_a_late_catalogue_success_or_failure() {
        let refresh = RefreshState::default();
        let catalogue = refresh.begin();
        let status = refresh.begin();
        assert!(refresh.accept(status));
        assert!(!refresh.accept(catalogue));
        assert!(
            !refresh.accept(status),
            "a queued result is applied only once"
        );
        assert!(refresh.accept(refresh.begin()));
    }

    #[test]
    fn rejected_startup_read_rearms_an_already_consumed_notification() {
        tauri::async_runtime::block_on(async {
            let refresh = RefreshState::default();
            let starting = refresh.begin();
            refresh.invalidate();
            refresh.changed().await; // no cache; the catalogue reader is busy
            assert!(!refresh.accept(starting));
            tokio::time::timeout(Duration::from_millis(100), refresh.changed())
                .await
                .expect("startup must retry without waiting for the polling interval");
            assert!(refresh.accept(refresh.begin()));
        });
    }

    #[test]
    fn state_change_during_read_discards_old_snapshot_and_keeps_a_wakeup() {
        tauri::async_runtime::block_on(async {
            let refresh = RefreshState::default();
            refresh.invalidate();
            refresh.changed().await;
            let reading_active = refresh.begin();
            refresh.invalidate(); // focus loss / Off / renderer failure
            refresh.invalidate(); // another transition while that read is pending
            assert!(!refresh.accept(reading_active));
            tokio::time::timeout(Duration::from_millis(100), refresh.changed())
                .await
                .expect("a transition during the read must trigger the next read");
            assert!(refresh.accept(refresh.begin()));
            assert!(
                tokio::time::timeout(Duration::from_millis(10), refresh.changed())
                    .await
                    .is_err(),
                "multiple transitions should coalesce"
            );
        });
    }
}
