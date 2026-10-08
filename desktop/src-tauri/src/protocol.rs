use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{io::BufRead, sync::Mutex};

pub const MAX_LINE: usize = 2 * 1024 * 1024 + 1; // Go response limit plus newline
pub const MAX_REQUEST: usize = 120 * 1024; // room for v/id envelope under Go 128 KiB limit
pub const MAX_PENDING: usize = 16;

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Request {
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(default)]
    pub payload: Value,
}

#[derive(Clone, Debug, Serialize)]
pub struct TerminalFailure {
    code: String,
    message: String,
}
#[derive(Default)]
pub struct TerminalErrors(Mutex<Option<TerminalFailure>>);
impl TerminalErrors {
    pub fn record_event(&self, event: &Value) -> bool {
        if event["event"] != "fatal" {
            return false;
        }
        let code = event["data"]["code"]
            .as_str()
            .filter(|s| !s.is_empty())
            .unwrap_or("engine_failed");
        let message = event["data"]["message"]
            .as_str()
            .filter(|s| !s.is_empty())
            .unwrap_or("Engine reported a fatal error");
        // A concrete Go failure supersedes a transport summary, even if no UI
        // listener existed when the event arrived. Never retain the raw event.
        *self.0.lock().unwrap() = Some(TerminalFailure {
            code: bounded(code, 64),
            message: bounded(message, 2048),
        });
        true
    }
    pub fn fallback(&self, code: &str, message: &str) {
        let mut failure = self.0.lock().unwrap();
        if failure.is_none() {
            *failure = Some(TerminalFailure {
                code: bounded(code, 64),
                message: bounded(message, 2048),
            });
        }
    }
    pub fn current(&self) -> Option<TerminalFailure> {
        self.0.lock().unwrap().clone()
    }
    pub fn rejection(&self, fallback: &str) -> String {
        self.current()
            .map(|failure| serde_json::json!({"error":failure}).to_string())
            .unwrap_or_else(|| fallback.into())
    }
}
fn bounded(value: &str, maximum: usize) -> String {
    if value.len() <= maximum {
        return value.into();
    }
    let mut end = maximum - 3;
    while !value.is_char_boundary(end) {
        end -= 1;
    }
    format!("{}...", &value[..end])
}

pub fn pending_limit(kind: &str) -> usize {
    MAX_PENDING
        + match kind {
            "emergency" => 1,
            "quit" => 2,
            _ => 0,
        }
}

pub fn mutates(kind: &str) -> bool {
    !matches!(
        kind,
        "snapshot" | "sources" | "preview" | "diagnostics" | "shaders" | "shader_source"
    )
}

pub fn allowed_during_terminal(kind: &str) -> bool {
    matches!(
        kind,
        "snapshot" | "sources" | "shaders" | "shader_source" | "emergency"
    )
}

pub fn validate(request: &Request) -> Result<(), String> {
    if !matches!(
        request.kind.as_str(),
        "snapshot"
            | "sources"
            | "apply"
            | "toggle"
            | "disable"
            | "emergency"
            | "restore"
            | "confirm"
            | "preview"
            | "diagnostics"
            | "reload"
            | "shaders"
            | "shader_import"
            | "shader_source"
    ) {
        return Err("Unsupported engine request".into());
    }
    if !request.payload.is_null() && !request.payload.is_object() {
        return Err("Request payload must be an object".into());
    }
    if serde_json::to_vec(request)
        .map_err(|e| e.to_string())?
        .len()
        > MAX_REQUEST
    {
        return Err("Request exceeds 120 KiB".into());
    }
    Ok(())
}

pub fn validate_response(value: &Value) -> Result<(), String> {
    if value.get("v").and_then(Value::as_u64) != Some(1) {
        return Err("Unsupported engine protocol version".into());
    }
    if let Some(id) = value.get("id") {
        if id.as_str().is_none_or(|s| s.is_empty() || s.len() > 64)
            || !value.get("ok").is_some_and(Value::is_boolean)
        {
            return Err("Invalid engine response envelope".into());
        }
    } else if value.get("event").and_then(Value::as_str).is_none() {
        return Err("Invalid engine event envelope".into());
    }
    Ok(())
}

pub fn response_result(value: Value) -> Result<Value, String> {
    if value["ok"] == true {
        Ok(value.get("result").cloned().unwrap_or(Value::Null))
    } else {
        // An applied/save error can include an authoritative snapshot. Preserve it.
        Err(value.to_string())
    }
}
pub fn read_line<R: BufRead>(reader: &mut R) -> Result<Option<Vec<u8>>, String> {
    let mut line = Vec::new();
    loop {
        let chunk = reader.fill_buf().map_err(|e| e.to_string())?;
        if chunk.is_empty() {
            return if line.is_empty() {
                Ok(None)
            } else {
                Err("Truncated engine protocol line".into())
            };
        }
        let end = chunk
            .iter()
            .position(|b| *b == b'\n')
            .map(|n| n + 1)
            .unwrap_or(chunk.len());
        if line.len() + end > MAX_LINE {
            return Err("Engine protocol line exceeds 2 MiB".into());
        }
        let done = chunk[end - 1] == b'\n';
        line.extend_from_slice(&chunk[..end]);
        reader.consume(end);
        if done {
            return Ok(Some(line));
        }
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    #[test]
    fn fatal_before_listener_survives_exit_and_subsequent_request() {
        let errors = TerminalErrors::default();
        // No frontend is listening. The pipe reader still records the Go event.
        errors.record_event(&json!({"v":1,"event":"fatal","data":{"code":"engine_already_running","message":"Another LumaTape engine is already running","ignored_state":{"secret":"not retained"}}}));
        errors.fallback("engine_exited", "Engine is not running");
        let later: Value =
            serde_json::from_str(&errors.rejection("Engine is not running")).unwrap();
        assert_eq!(later["error"]["code"], "engine_already_running");
        assert_eq!(
            later["error"]["message"],
            "Another LumaTape engine is already running"
        );
        assert_eq!(later["error"].as_object().unwrap().len(), 2);
    }
    #[test]
    fn retained_failure_is_utf8_bounded_and_go_reason_supersedes_transport() {
        let errors = TerminalErrors::default();
        errors.fallback("input_closed", "pipe closed");
        errors.record_event(&json!({"event":"fatal","data":{"code":"Ж".repeat(80),"message":"Причина💿".repeat(400)}}));
        let failure = errors.current().unwrap();
        assert!(failure.code.len() <= 64 && failure.message.len() <= 2048);
        assert!(failure.message.ends_with("..."));
        assert!(!errors.record_event(&json!({"event":"ready","data":{}})));
        assert_eq!(errors.current().unwrap().message, failure.message);
    }
    #[test]
    fn terminal_slot_survives_normal_and_emergency_queue_saturation() {
        assert_eq!(pending_limit("preview"), 16);
        assert_eq!(pending_limit("emergency"), 17);
        assert_eq!(pending_limit("quit"), 18);
    }
    #[test]
    fn applied_error_preserves_authoritative_result() {
        let response = json!({"v":1,"id":"4","ok":false,"error":{"code":"save_failed","message":"disk error","applied":true,"unsaved":true},"result":{"config":{"enabled":true}}});
        let rejected = response_result(response.clone()).unwrap_err();
        assert_eq!(serde_json::from_str::<Value>(&rejected).unwrap(), response);
        assert_eq!(
            response_result(json!({"ok":true,"result":{"config":1}})).unwrap(),
            json!({"config":1})
        );
    }
    #[test]
    fn reader_requires_newline_and_bounds_memory() {
        use std::io::Cursor;
        assert_eq!(
            read_line(&mut Cursor::new(b"ok\n")).unwrap(),
            Some(b"ok\n".to_vec())
        );
        assert!(read_line(&mut Cursor::new(b"partial")).is_err());
        assert!(read_line(&mut Cursor::new(vec![b'x'; MAX_LINE + 1])).is_err());
        assert_eq!(read_line(&mut Cursor::new(b"")).unwrap(), None);
    }
    #[test]
    fn request_boundary_is_allowlisted_and_bounded() {
        for kind in ["snapshot", "apply", "preview", "disable"] {
            assert!(validate(&Request {
                kind: kind.into(),
                payload: json!({})
            })
            .is_ok());
        }
        // Idempotent filter-off is an ordinary request. It does not consume
        // the priority slots reserved for emergency and terminal cleanup.
        assert_eq!(pending_limit("disable"), MAX_PENDING);
        assert!(validate(&Request {
            kind: "disable".into(),
            payload: json!(false),
        })
        .is_err());
        for kind in ["quit", "ui_state", "spawn", "shell", "open_file"] {
            assert!(validate(&Request {
                kind: kind.into(),
                payload: Value::Null
            })
            .is_err());
        }
        assert!(validate(&Request {
            kind: "apply".into(),
            payload: json!("raw")
        })
        .is_err());
        assert!(validate(&Request {
            kind: "apply".into(),
            payload: json!({"big":"x".repeat(MAX_REQUEST)})
        })
        .is_err());
    }
    #[test]
    fn shader_reads_are_bounded_and_import_keeps_mutation_and_terminal_gates() {
        for kind in ["shaders", "shader_source", "shader_import"] {
            assert!(validate(&Request {
                kind: kind.into(),
                payload: json!({})
            })
            .is_ok());
            assert_eq!(pending_limit(kind), MAX_PENDING);
            assert!(validate(&Request {
                kind: kind.into(),
                payload: json!({"source":"x".repeat(MAX_REQUEST)})
            })
            .is_err());
            assert!(validate(&Request {
                kind: kind.into(),
                payload: json!("C:\\arbitrary.frag")
            })
            .is_err());
        }
        for kind in ["shaders", "shader_source"] {
            assert!(!mutates(kind));
            assert!(allowed_during_terminal(kind));
        }
        assert!(mutates("shader_import"));
        assert!(!allowed_during_terminal("shader_import"));
        assert!(!allowed_during_terminal("apply"));
        assert!(!allowed_during_terminal("preview"));
        assert!(allowed_during_terminal("emergency"));
    }
    #[test]
    fn version_and_response_identity_are_required() {
        assert!(validate_response(&json!({"v":1,"id":"1","ok":true,"result":{}})).is_ok());
        assert!(validate_response(&json!({"v":1,"event":"ready","data":{}})).is_ok());
        for value in [
            json!({"v":2,"id":"1","ok":true}),
            json!({"v":1,"id":1,"ok":true}),
            json!({"v":1,"id":"1"}),
            json!({"v":1}),
        ] {
            assert!(validate_response(&value).is_err());
        }
    }
}
