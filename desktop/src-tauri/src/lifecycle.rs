/// Explicit user quit may dismiss an already-dead engine, while preserving a
/// failed-cleanup exit code. An unknown/running process must still block exit.
/// Updater shutdown deliberately does not use this policy.
pub fn explicit_quit_exit_code(
    cleanup: Result<(), String>,
    confirmed_process_exit: Option<i32>,
) -> Result<i32, String> {
    match cleanup {
        Ok(()) => Ok(0),
        Err(_) if confirmed_process_exit.is_some() => Ok(1),
        Err(error) => Err(error),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn explicit_quit_can_dismiss_failed_engine_after_actual_exit() {
        assert_eq!(
            explicit_quit_exit_code(Err("cleanup failed".into()), Some(1)),
            Ok(1)
        );
        // Exit 0 without cleanup acknowledgement must not be reported as success.
        assert_eq!(
            explicit_quit_exit_code(Err("missing acknowledgement".into()), Some(0)),
            Ok(1)
        );
        assert_eq!(explicit_quit_exit_code(Ok(()), Some(0)), Ok(0));
    }
    #[test]
    fn timeout_or_wait_failure_cannot_authorize_exit_of_running_engine() {
        assert_eq!(
            explicit_quit_exit_code(Err("timed out".into()), None),
            Err("timed out".into())
        );
    }
}
