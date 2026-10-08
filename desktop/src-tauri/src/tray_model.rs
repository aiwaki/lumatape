//! Pure native-menu projection and mutation planning. The engine remains the
//! authority for OS capability/registration/geometry checks. No title is used
//! as source identity, and every apply carries both emergency and config CAS.
use crate::i18n::text;
use serde_json::{json, Value};
use std::collections::HashSet;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub id: String,
    pub label: String,
    pub enabled: bool,
    pub checked: Option<bool>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Group {
    pub label: String,
    pub entries: Vec<Entry>,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct MenuModel {
    pub emergency_sequence: u64,
    pub status: String,
    /// Dispatch uses semantic state, never translated text. The controller
    /// additionally treats a pending operation or absent model as an Off intent.
    pub wants_off: bool,
    pub power: Entry,
    pub sources: Vec<Entry>,
    pub effects: Vec<Entry>,
    pub settings: Vec<Group>,
    pub confirmation_pending: bool,
}
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Action {
    Power,
    Off,
    Source(String),
    Monitor(u32),
    Preset(String),
    Shader(String),
    Shape(String),
    Format(String),
    InputMode(String),
    Scale(String),
    Dar(String),
    Backend(String),
    Hotkeys(String),
}
#[derive(Clone, Debug, PartialEq)]
pub struct Mutation {
    pub kind: &'static str,
    pub payload: Value,
}

/// Escape Windows menu mnemonics and remove shortcut/control injection while
/// preserving ordinary Unicode names. The identity always uses unmodified data.
pub fn menu_label(value: &str) -> String {
    value
        .chars()
        .map(|c| if c.is_control() { ' ' } else { c })
        .collect::<String>()
        .replace('&', "&&")
}
fn hex(value: &str) -> String {
    use std::fmt::Write;
    let mut out = String::with_capacity(value.len() * 2);
    for byte in value.as_bytes() {
        let _ = write!(out, "{byte:02x}");
    }
    out
}
fn unhex(value: &str) -> Option<String> {
    if value.is_empty() || value.len() > 32768 || value.len() % 2 != 0 {
        return None;
    }
    let mut bytes = Vec::with_capacity(value.len() / 2);
    for pair in value.as_bytes().chunks_exact(2) {
        let digit = |b| match b {
            b'0'..=b'9' => Some(b - b'0'),
            b'a'..=b'f' => Some(b - b'a' + 10),
            _ => None,
        };
        bytes.push(digit(pair[0])? * 16 + digit(pair[1])?);
    }
    String::from_utf8(bytes).ok()
}
fn hash_id(value: &str) -> bool {
    value.len() == 64
        && value
            .bytes()
            .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}
fn one_of(value: &str, choices: &[&str]) -> bool {
    choices.contains(&value)
}
impl Action {
    pub fn id(&self) -> String {
        let suffix = match self {
            Self::Power => "power".into(),
            Self::Off => "off".into(),
            Self::Source(key) => format!("source:{}", hex(key)),
            Self::Monitor(index) => format!("monitor:{index}"),
            Self::Preset(name) => format!("preset:{}", hex(name)),
            Self::Shader(id) => format!("shader:{id}"),
            Self::Shape(v) => format!("shape:{v}"),
            Self::Format(v) => format!("format:{v}"),
            Self::InputMode(v) => format!("input:{v}"),
            Self::Scale(v) => format!("scale:{v}"),
            Self::Dar(v) => format!("dar:{v}"),
            Self::Backend(v) => format!("backend:{v}"),
            Self::Hotkeys(v) => format!("hotkeys:{v}"),
        };
        format!("model:{suffix}")
    }
}
pub fn parse_action(id: &str) -> Option<Action> {
    let value = id.strip_prefix("model:")?;
    if value == "power" {
        return Some(Action::Power);
    }
    if value == "off" {
        return Some(Action::Off);
    }
    let (kind, value) = value.split_once(':')?;
    Some(match kind {
        "source" => Action::Source(unhex(value)?),
        "monitor" => {
            let index = value.parse::<u32>().ok()?;
            if index > 1024 || index.to_string() != value {
                return None;
            }
            Action::Monitor(index)
        }
        "preset" => Action::Preset(unhex(value)?),
        "shader" if hash_id(value) => Action::Shader(value.into()),
        "shape" if one_of(value, &["flat", "rounded", "convex"]) => Action::Shape(value.into()),
        "format" if one_of(value, &["off", "mask", "window", "system"]) => {
            Action::Format(value.into())
        }
        "input" if one_of(value, &["mouse-exact", "keyboard-gamepad"]) => {
            Action::InputMode(value.into())
        }
        "scale" if one_of(value, &["fit", "crop", "stretch"]) => Action::Scale(value.into()),
        "dar" if one_of(value, &["auto", "4:3"]) => Action::Dar(value.into()),
        "backend"
            if one_of(
                value,
                &["full-auto", "full-gpu", "full-compatibility", "overlay"],
            ) =>
        {
            Action::Backend(value.into())
        }
        "hotkeys" if one_of(value, &["standard", "compact"]) => Action::Hotkeys(value.into()),
        _ => return None,
    })
}
fn bad(field: &str) -> String {
    crate::localized!(
        "Движок вернул неполное или неизвестное состояние ({field}). Подождите немного и снова откройте меню.",
        "The engine returned an incomplete or unknown state ({field}). Wait a moment and reopen the menu."
    )
}
fn string<'a>(value: &'a Value, pointer: &str) -> Result<&'a str, String> {
    value
        .pointer(pointer)
        .and_then(Value::as_str)
        .ok_or_else(|| bad(pointer))
}
fn boolean(value: &Value, pointer: &str) -> Result<bool, String> {
    value
        .pointer(pointer)
        .and_then(Value::as_bool)
        .ok_or_else(|| bad(pointer))
}
fn number(value: &Value, pointer: &str, min: f64, max: f64) -> Result<f64, String> {
    let n = value
        .pointer(pointer)
        .and_then(Value::as_f64)
        .ok_or_else(|| bad(pointer))?;
    if n.is_finite() && n >= min && n <= max {
        Ok(n)
    } else {
        Err(bad(pointer))
    }
}
fn enumeration<'a>(value: &'a Value, pointer: &str, choices: &[&str]) -> Result<&'a str, String> {
    let s = string(value, pointer)?;
    if one_of(s, choices) {
        Ok(s)
    } else {
        Err(bad(pointer))
    }
}
fn array<'a>(value: &'a Value, pointer: &str) -> Result<&'a [Value], String> {
    value
        .pointer(pointer)
        .and_then(Value::as_array)
        .map(Vec::as_slice)
        .ok_or_else(|| bad(pointer))
}
fn effect_fields(value: &Value) -> Result<(), String> {
    for field in [
        "/crt/scanlines",
        "/crt/mask",
        "/crt/bloom",
        "/crt/softness",
        "/crt/vignette",
        "/crt/curvature",
        "/vhs/chroma_bleed",
        "/vhs/noise",
        "/vhs/jitter",
        "/vhs/tracking",
    ] {
        number(value, field, 0., 1.)?;
    }
    Ok(())
}
fn validate_config(c: &Value) -> Result<(), String> {
    if c["version"].as_u64() != Some(1) {
        return Err(bad("config.version"));
    }
    enumeration(c, "/mode", &["full", "overlay"])?;
    boolean(c, "/enabled")?;
    enumeration(c, "/capture/transfer", &["auto", "gpu", "compatibility"])?;
    enumeration(c, "/target/kind", &["window", "monitor"])?;
    if c["target"]["monitor"]
        .as_u64()
        .filter(|n| *n <= 1024)
        .is_none()
    {
        return Err(bad("target.monitor"));
    }
    let title = string(c, "/target/window_title")?;
    if title.len() > 4096
        || title.contains('\0')
        || (c["target"]["kind"] == "window" && title.trim().is_empty())
    {
        return Err(bad("target.window_title"));
    }
    enumeration(
        c,
        "/preset",
        &[
            "Subtle CRT",
            "CRT Classic",
            "Soft TV",
            "VHS Light",
            "VHS Tape",
            "Custom",
        ],
    )?;
    number(c, "/effects/intensity", 0., 1.)?;
    boolean(c, "/effects/freeze_noise")?;
    if c["effects"]["noise_seed"]
        .as_u64()
        .filter(|n| *n <= u32::MAX.into())
        .is_none()
    {
        return Err(bad("effects.noise_seed"));
    }
    effect_fields(&c["effects"])?;
    enumeration(c, "/screen/shape", &["flat", "rounded", "convex"])?;
    number(c, "/screen/corner_radius", 0., 0.2)?;
    number(c, "/screen/curvature", 0., 1.)?;
    number(c, "/screen/glass", 0., 1.)?;
    boolean(c, "/aspect/enabled")?;
    enumeration(c, "/aspect/method", &["mask", "window", "system"])?;
    enumeration(c, "/aspect/scale", &["fit", "crop", "stretch"])?;
    number(c, "/aspect/source_dar", 0., 100.)?;
    enumeration(c, "/input_mode", &["mouse-exact", "keyboard-gamepad"])?;
    let id = string(c, "/shader/id")?;
    if !id.is_empty() && !hash_id(id) {
        return Err(bad("shader.id"));
    }
    let params = array(c, "/shader/params")?;
    if params.len() != 8
        || params.iter().any(|p| {
            p.as_f64()
                .is_none_or(|v| !v.is_finite() || v.abs() > 1e6 || (id.is_empty() && v != 0.))
        })
    {
        return Err(bad("shader.params"));
    }
    let toggle = string(c, "/hotkeys/toggle")?;
    let emergency = string(c, "/hotkeys/emergency")?;
    if toggle.trim().is_empty()
        || emergency.trim().is_empty()
        || toggle.eq_ignore_ascii_case(emergency)
    {
        return Err(bad("hotkeys"));
    }
    Ok(())
}
fn combinations(c: &Value) -> Result<(), String> {
    let full = c["mode"] == "full";
    let window = c["target"]["kind"] == "window";
    let exact = c["input_mode"] == "mouse-exact";
    if !c["shader"]["id"].as_str().unwrap_or_default().is_empty() && !full {
        return Err(text(
            "Свой шейдер требует полной обработки изображения.",
            "Custom shaders require full image processing.",
        )
        .into());
    }
    if full && !window {
        return Err(text(
            "Для полной обработки выберите окно игры.",
            "Select a game window for full image processing.",
        )
        .into());
    }
    if exact && (c["aspect"]["scale"] != "fit" || c["aspect"]["source_dar"].as_f64() != Some(0.)) {
        return Err(text("Для обрезки, растяжения и изменения пропорций выберите «Искажения изображения → Разрешить произвольные искажения». Клики могут не совпадать с изображением.", "To crop, stretch, or change the aspect ratio, choose Image distortion → Allow arbitrary distortion. Clicks may not match the image.").into());
    }
    if c["aspect"]["enabled"] == true && c["aspect"]["method"] == "window" && !window {
        return Err(text("Сначала выберите окно игры.", "Select a game window first.").into());
    }
    if c["screen"]["shape"] == "convex" && !full {
        return Err(text(
            "Выпуклый экран требует полной обработки изображения.",
            "A curved screen requires full image processing.",
        )
        .into());
    }
    Ok(())
}
struct Snapshot<'a> {
    config: &'a Value,
    sequence: u64,
    should_stop: bool,
    recovery: bool,
    confirmation: bool,
}
impl<'a> Snapshot<'a> {
    fn read(raw: &'a Value) -> Result<Self, String> {
        let sequence = raw["emergency_sequence"]
            .as_u64()
            .ok_or_else(|| bad("emergency_sequence"))?;
        let config = &raw["config"];
        validate_config(config)?;
        combinations(config)?;
        let phase = enumeration(
            raw,
            "/runtime/phase",
            &[
                "starting",
                "active",
                "ready",
                "disabled",
                "bypass",
                "paused-settings",
                "paused-stale",
                "waiting-source",
                "paused-focus",
                "paused-moving",
                "waiting-frame",
                "source-closed",
                "error",
                "recovery-error",
            ],
        )?;
        enumeration(
            raw,
            "/runtime/backend",
            &["", "lightweight", "full-gpu", "full-compatibility"],
        )?;
        let runtime_on = boolean(raw, "/runtime/enabled")?;
        boolean(raw, "/runtime/effect_active")?;
        boolean(raw, "/runtime/surface_visible")?;
        let format = boolean(raw, "/runtime/format_active")?;
        let recovery = boolean(raw, "/runtime/recovery_pending")? || phase == "recovery-error";
        let unsaved = boolean(raw, "/runtime/unsaved")?;
        for path in ["/runtime/gpu/state", "/runtime/compatibility/state"] {
            enumeration(
                raw,
                path,
                &["unknown", "available", "unavailable", "failed"],
            )?;
        }
        let confirmation = match raw.get("confirmation_deadline") {
            None | Some(Value::Null) => false,
            Some(Value::String(s)) if !s.is_empty() => true,
            _ => return Err(bad("confirmation_deadline")),
        };
        if let Some(source) = raw.get("source").filter(|s| !s.is_null()) {
            source_key(source)?;
        }
        let should_stop = config["enabled"] == true
            || config["aspect"]["enabled"] == true
            || runtime_on
            || format
            || recovery
            || unsaved
            || confirmation;
        Ok(Self {
            config,
            sequence,
            should_stop,
            recovery,
            confirmation,
        })
    }
}
fn source_key(source: &Value) -> Result<String, String> {
    let hwnd = string(source, "/hwnd")?;
    let handle = hwnd
        .strip_prefix("0x")
        .and_then(|h| u64::from_str_radix(h, 16).ok());
    let pid = source["pid"]
        .as_u64()
        .filter(|pid| *pid > 0 && *pid <= u32::MAX.into());
    let created = string(source, "/process_created")?;
    let title = string(source, "/title")?;
    if handle.is_none_or(|h| h == 0)
        || pid.is_none()
        || (!created.is_empty() && created.parse::<u64>().is_err())
        || title.trim().is_empty()
        || title.len() > 4096
        || title.contains('\0')
    {
        return Err(bad("source"));
    }
    Ok(format!("window:{hwnd}:{}:{created}", pid.unwrap()))
}
fn windows(sources: &Value) -> Result<&[Value], String> {
    let windows = array(sources, "/windows")?;
    let mut seen = HashSet::new();
    for window in windows {
        if !seen.insert(source_key(window)?) {
            return Err(bad("duplicate source identity"));
        }
    }
    Ok(windows)
}
fn descriptors(shaders: &Value) -> Result<&[Value], String> {
    let items = array(shaders, "/items")?;
    if items.len() > 128 {
        return Err(bad("shader count"));
    }
    let mut seen = HashSet::new();
    for item in items {
        let id = string(item, "/id")?;
        if !hash_id(id) || !seen.insert(id) || item["version"].as_u64() != Some(1) {
            return Err(bad("shader identity"));
        }
        let name = string(item, "/name")?;
        if name.trim().is_empty() || name.chars().count() > 80 {
            return Err(bad("shader name"));
        }
        enumeration(item, "/coordinates", &["preserve", "warp"])?;
        let params = array(item, "/parameters")?;
        if params.len() > 8
            || params.iter().any(|p| {
                p["default"]
                    .as_f64()
                    .is_none_or(|n| !n.is_finite() || n.abs() > 1e6)
            })
        {
            return Err(bad("shader parameters"));
        }
    }
    Ok(items)
}
fn presets(snapshot: &Value) -> Result<&[Value], String> {
    let values = array(snapshot, "/presets")?;
    let mut seen = HashSet::new();
    for preset in values {
        let name = string(preset, "/name")?;
        if !one_of(
            name,
            &[
                "Subtle CRT",
                "CRT Classic",
                "Soft TV",
                "VHS Light",
                "VHS Tape",
                "Custom",
            ],
        ) || !seen.insert(name)
        {
            return Err(bad("preset name"));
        }
        effect_fields(&preset["effects"])?;
    }
    Ok(values)
}
fn backend_check(c: &Value, snapshot: &Value) -> Result<(), String> {
    if c["mode"] != "full" {
        return Ok(());
    }
    let gpu = snapshot["runtime"]["gpu"]["state"] == "unavailable";
    let cpu = snapshot["runtime"]["compatibility"]["state"] == "unavailable";
    match c["capture"]["transfer"].as_str() {
        Some("gpu") if gpu => Err(text(
            "Обработка через GPU недоступна. Выберите «Автоматически» или режим совместимости CPU.",
            "GPU processing is unavailable. Choose Automatic or CPU compatibility mode.",
        )
        .into()),
        Some("compatibility") if cpu => Err(text(
            "Режим совместимости CPU недоступен.",
            "CPU compatibility mode is unavailable.",
        )
        .into()),
        Some("auto") if gpu && cpu => Err(text(
            "Полная обработка недоступна. Выберите лёгкий режим.",
            "Full image processing is unavailable. Choose Lightweight.",
        )
        .into()),
        _ => Ok(()),
    }
}
fn full_effect(c: &mut Value) {
    c["effects"]["intensity"] = json!(1);
    if c["mode"] == "overlay" && c["target"]["kind"] == "window" {
        c["mode"] = json!("full");
        c["capture"]["transfer"] = json!("auto");
    }
}

/// Call with a fresh snapshot only after checking its sequence against the
/// clicked menu's sequence. Never retry a rejected CAS against a newer config.
/// `Off` intentionally works even when the read model is unavailable.
pub fn plan(
    action: &Action,
    snapshot: &Value,
    sources: &Value,
    shaders: &Value,
) -> Result<Mutation, String> {
    if *action == Action::Off {
        return Ok(Mutation {
            kind: "emergency",
            payload: Value::Null,
        });
    }
    if parse_action(&action.id()).as_ref() != Some(action) {
        return Err(text("Неизвестная команда меню.", "Unknown menu command.").into());
    }
    let state = Snapshot::read(snapshot)?;
    if *action == Action::Power && state.should_stop {
        return plan(&Action::Off, snapshot, sources, shaders);
    }
    if state.recovery || state.confirmation {
        return Err(text(
            "Сначала завершите восстановление или подтвердите видеорежим.",
            "Complete restoration or confirm the display mode first.",
        )
        .into());
    }
    let available = windows(sources)?;
    let mut config = state.config.clone();
    let mut source = snapshot.get("source").filter(|s| !s.is_null()).cloned();
    match action {
        Action::Off => unreachable!(),
        Action::Power => {
            if config["target"]["kind"] != "window" {
                return Err(text(
                    "Выберите окно игры. Запуск на всём мониторе не выполняется автоматически.",
                    "Select a game window. Full-monitor capture does not start automatically.",
                )
                .into());
            }
            config["enabled"] = json!(true);
            config["effects"]["intensity"] = json!(1);
            if config["mode"] == "overlay" && config["capture"]["transfer"] == "auto" {
                config["mode"] = json!("full");
            }
        }
        Action::Source(key) => {
            let chosen = available
                .iter()
                .find(|s| source_key(s).as_ref() == Ok(key))
                .ok_or(text(
                    "Окно закрыто или изменилось. Подождите немного, снова откройте меню «Игра» и выберите окно.",
                    "The window closed or changed. Wait a moment, reopen the Game menu, and select a window again.",
                ))?;
            config["target"] = json!({"kind":"window","monitor":0,"window_title":chosen["title"]});
            source = Some(chosen.clone());
        }
        Action::Monitor(index) => {
            let monitor = array(sources, "/monitors")?
                .iter()
                .any(|m| m["index"].as_u64() == Some((*index).into()));
            if !monitor {
                return Err(text(
                    "Монитор больше недоступен.",
                    "The display is no longer available.",
                )
                .into());
            }
            config["target"] = json!({"kind":"monitor","monitor":index,"window_title":""});
            source = None;
        }
        Action::Preset(name) => {
            let preset = presets(snapshot)?
                .iter()
                .find(|p| p["name"].as_str() == Some(name))
                .ok_or(text(
                    "Этот встроенный эффект недоступен.",
                    "This built-in effect is unavailable.",
                ))?;
            if name != "Custom" {
                config["effects"]["crt"] = preset["effects"]["crt"].clone();
                config["effects"]["vhs"] = preset["effects"]["vhs"].clone();
            }
            config["preset"] = json!(name);
            config["shader"] = json!({"id":"","params":[0,0,0,0,0,0,0,0]});
            full_effect(&mut config);
        }
        Action::Shader(id) => {
            if config["target"]["kind"] != "window" {
                return Err(text(
                    "Для своего эффекта выберите окно игры.",
                    "Select a game window to use a custom effect.",
                )
                .into());
            }
            let shader = descriptors(shaders)?
                .iter()
                .find(|s| s["id"] == *id)
                .ok_or(text(
                    "Эффект больше недоступен. Импортируйте файл заново.",
                    "The effect is no longer available. Import the file again.",
                ))?;
            if shader["coordinates"] == "warp" && config["input_mode"] != "keyboard-gamepad" {
                return Err(text("Этот шейдер смещает изображение. Выберите «Искажения изображения → Разрешить произвольные искажения». Клики могут не совпадать с изображением.", "This shader moves the image. Choose Image distortion → Allow arbitrary distortion. Clicks may not match the image.").into());
            }
            // Re-selecting the current immutable asset must preserve saved values.
            if config["shader"]["id"] != *id {
                let mut params = vec![json!(0); 8];
                for (i, parameter) in array(shader, "/parameters")?.iter().enumerate() {
                    params[i] = parameter["default"].clone();
                }
                config["shader"] = json!({"id":id,"params":params});
            }
            full_effect(&mut config);
        }
        Action::Shape(shape) => config["screen"]["shape"] = json!(shape),
        Action::Format(method) => {
            config["aspect"]["enabled"] = json!(method != "off");
            if method != "off" {
                config["aspect"]["method"] = json!(method);
            }
        }
        Action::InputMode(mode) => config["input_mode"] = json!(mode),
        Action::Scale(scale) => config["aspect"]["scale"] = json!(scale),
        Action::Dar(dar) => {
            config["aspect"]["source_dar"] = if dar == "auto" {
                json!(0)
            } else {
                json!(4. / 3.)
            }
        }
        Action::Backend(backend) => {
            config["mode"] = json!(if backend == "overlay" {
                "overlay"
            } else {
                "full"
            });
            config["capture"]["transfer"] = json!(match backend.as_str() {
                "full-auto" => "auto",
                "full-compatibility" => "compatibility",
                _ => "gpu",
            });
        }
        Action::Hotkeys(profile) => {
            config["hotkeys"] = if profile == "compact" {
                json!({"toggle":"Ctrl+Shift+9","emergency":"Ctrl+Shift+0"})
            } else {
                json!({"toggle":"Ctrl+Alt+F9","emergency":"Ctrl+Alt+F10"})
            };
        }
    }
    validate_config(&config)?;
    combinations(&config)?;
    // An unavailable capture backend must not prevent repairing hotkeys or
    // saving unrelated preferences. Match the engine's preflight boundary:
    // selecting/starting capture and activating window/display geometry.
    let activating_format = config["aspect"]["enabled"] == true
        && (state.config["aspect"]["enabled"] != true
            || config["aspect"]["method"] != state.config["aspect"]["method"]);
    let needs_backend = matches!(
        action,
        Action::Power | Action::Backend(_) | Action::Source(_) | Action::Monitor(_)
    ) || config["mode"] != state.config["mode"]
        || config["capture"] != state.config["capture"]
        || config["target"] != state.config["target"]
        || activating_format;
    if needs_backend {
        backend_check(&config, snapshot)?;
    }
    // Warp assets must not become exact-mouse merely by changing another menu.
    if !config["shader"]["id"]
        .as_str()
        .unwrap_or_default()
        .is_empty()
        && config["input_mode"] == "mouse-exact"
    {
        let current = descriptors(shaders)?
            .iter()
            .find(|s| s["id"] == config["shader"]["id"])
            .ok_or(text(
                "Сведения о текущем шейдере недоступны. Выберите другой эффект.",
                "Current shader details are unavailable. Choose another effect.",
            ))?;
        if current["coordinates"] == "warp" {
            return Err(
                text("Текущий шейдер смещает изображение. Для режима «Точные клики» сначала выберите эффект без произвольных искажений.", "The current shader moves the image. To use Accurate clicks, first choose an effect without arbitrary distortion.").into(),
            );
        }
    }
    let needs_live_source = *action == Action::Power
        || matches!(action, Action::Source(_))
        || config["enabled"] == true
        || config["aspect"]["enabled"] == true
        || config["mode"] != state.config["mode"]
        || config["capture"] != state.config["capture"];
    if config["target"]["kind"] == "window" {
        if let Some(selected) = &source {
            let key = source_key(selected)?;
            let live = available
                .iter()
                .find(|s| source_key(s).as_ref() == Ok(&key));
            if let Some(live) = live {
                source = Some(live.clone());
            } else if needs_live_source {
                return Err(text(
                    "Выбранное окно закрыто. Выберите новое окно игры.",
                    "The selected window closed. Select another game window.",
                )
                .into());
            } else {
                source = None;
            }
        } else if needs_live_source {
            return Err(text(
                "Выберите окно игры в меню «Игра».",
                "Select a game window in the Game menu.",
            )
            .into());
        }
    } else {
        source = None;
    }
    let mut payload = json!({"config":config,"expected_config":state.config,"expected_emergency_sequence":state.sequence});
    if let Some(source) = source {
        payload["source"] = source;
    }
    Ok(Mutation {
        kind: "apply",
        payload,
    })
}

pub fn build(snapshot: &Value, sources: &Value, shaders: &Value) -> Result<MenuModel, String> {
    let state = Snapshot::read(snapshot)?;
    let c = state.config;
    let available = windows(sources)?;
    let packs = descriptors(shaders)?;
    let builtins = presets(snapshot)?;
    let make = |action: Action, label: &str, checked: Option<bool>| Entry {
        id: action.id(),
        label: menu_label(label),
        enabled: plan(&action, snapshot, sources, shaders).is_ok(),
        checked,
    };
    let selected = snapshot
        .get("source")
        .filter(|s| !s.is_null())
        .map(source_key)
        .transpose()?;
    let source_entries = available
        .iter()
        .map(|source| {
            let key = source_key(source).expect("validated source");
            let title = source["title"].as_str().unwrap();
            let duplicates = available
                .iter()
                .filter(|s| s["title"] == source["title"])
                .count();
            let label = if duplicates > 1 {
                format!(
                    "{title} · PID {} · {}",
                    source["pid"],
                    source["hwnd"].as_str().unwrap()
                )
            } else {
                title.into()
            };
            make(
                Action::Source(key.clone()),
                &label,
                Some(selected.as_ref() == Some(&key)),
            )
        })
        .collect();
    let custom_active = !c["shader"]["id"].as_str().unwrap().is_empty();
    let mut effects: Vec<Entry> = builtins
        .iter()
        .filter(|p| p["name"] != "Custom" || c["preset"] == "Custom" && !custom_active)
        .map(|p| {
            let name = p["name"].as_str().unwrap();
            make(
                Action::Preset(name.into()),
                if name == "Custom" {
                    text("Сохранённый эффект", "Saved effect")
                } else {
                    name
                },
                Some(!custom_active && c["preset"] == name),
            )
        })
        .collect();
    for shader in packs {
        let id = shader["id"].as_str().unwrap();
        let name = shader["name"].as_str().unwrap();
        let duplicate = packs.iter().filter(|p| p["name"] == name).count() > 1
            || builtins.iter().any(|p| p["name"] == name);
        let label = if duplicate {
            format!("{name} · {}", &id[..8])
        } else {
            name.into()
        };
        effects.push(make(
            Action::Shader(id.into()),
            &label,
            Some(c["shader"]["id"] == id),
        ));
    }
    if custom_active && !packs.iter().any(|s| s["id"] == c["shader"]["id"]) {
        effects.push(Entry {
            id: "model:missing-effect".into(),
            label: text("Текущий эффект недоступен", "Current effect unavailable").into(),
            enabled: false,
            checked: Some(true),
        });
    }
    let group = |label: &str, entries: Vec<Entry>| Group {
        label: label.into(),
        entries,
    };
    let choices = |items: &[(&str, &str)], to_action: fn(String) -> Action, current: &str| {
        items
            .iter()
            .map(|(id, label)| make(to_action((*id).into()), label, Some(current == *id)))
            .collect::<Vec<Entry>>()
    };
    let dar = c["aspect"]["source_dar"].as_f64().unwrap();
    let mut dar_entries = choices(
        &[
            ("auto", text("Как в окне", "As in the window")),
            ("4:3", text("4:3 · например 320×200", "4:3 · e.g. 320×200")),
        ],
        Action::Dar,
        if dar == 0. {
            "auto"
        } else if dar == 4. / 3. {
            "4:3"
        } else {
            "custom"
        },
    );
    if dar != 0. && dar != 4. / 3. {
        dar_entries.push(Entry {
            id: "model:dar-profile".into(),
            label: crate::localized!("Из профиля · {dar:.4}", "From profile · {dar:.4}"),
            enabled: false,
            checked: Some(true),
        });
    }
    let backend = if c["mode"] == "full"
        || c["capture"]["transfer"] == "auto" && c["target"]["kind"] == "window"
    {
        format!("full-{}", c["capture"]["transfer"].as_str().unwrap())
    } else {
        "overlay".into()
    };
    let standard = c["hotkeys"] == json!({"toggle":"Ctrl+Alt+F9","emergency":"Ctrl+Alt+F10"});
    let compact = c["hotkeys"] == json!({"toggle":"Ctrl+Shift+9","emergency":"Ctrl+Shift+0"});
    let mut hotkeys = choices(
        &[
            ("standard", "Ctrl+Alt+F9 / Ctrl+Alt+F10"),
            ("compact", "Ctrl+Shift+9 / Ctrl+Shift+0"),
        ],
        Action::Hotkeys,
        if standard {
            "standard"
        } else if compact {
            "compact"
        } else {
            "custom"
        },
    );
    if !standard && !compact {
        hotkeys.insert(
            0,
            Entry {
                id: "model:hotkeys-current".into(),
                label: menu_label(&format!(
                    "{} / {}",
                    c["hotkeys"]["toggle"].as_str().unwrap(),
                    c["hotkeys"]["emergency"].as_str().unwrap()
                )),
                enabled: false,
                checked: Some(true),
            },
        );
    }
    // Describe the user's saved bindings, including custom profiles. These
    // rows are informational; selecting a profile below still uses normal CAS.
    hotkeys.splice(
        0..0,
        [
            (
                "toggle",
                text("Включить / выключить эффект", "Toggle effect"),
            ),
            (
                "emergency",
                text(
                    "Аварийно отключить и восстановить",
                    "Emergency off and restore",
                ),
            ),
        ]
        .map(|(key, action)| Entry {
            id: format!("model:hotkey-help-{key}"),
            label: menu_label(&format!(
                "{} — {action}",
                c["hotkeys"][key].as_str().unwrap()
            )),
            enabled: false,
            checked: None,
        }),
    );
    let mut format_entries = choices(
        &[
            ("off", text("Исходный формат", "Original aspect ratio")),
            ("system", text("Разрешение Windows…", "Windows resolution…")),
        ],
        Action::Format,
        if c["aspect"]["enabled"] == true {
            c["aspect"]["method"].as_str().unwrap()
        } else {
            "off"
        },
    );
    // Retired formats remain readable/restorable in legacy profiles. Show the
    // actual preference without offering to activate it or migrating on read.
    let retired_format = match c["aspect"]["method"].as_str() {
        Some("mask") => Some((
            "mask",
            text("Маска 4:3 · из профиля", "4:3 mask · from profile"),
        )),
        Some("window") => Some((
            "window",
            text("Окно 4:3 · из профиля", "4:3 window · from profile"),
        )),
        _ => None,
    };
    if let Some((method, label)) = retired_format.filter(|_| c["aspect"]["enabled"] == true) {
        format_entries.insert(
            0,
            Entry {
                id: format!("model:format-profile-{method}"),
                label: label.into(),
                enabled: false,
                checked: Some(true),
            },
        );
    }
    let mut distortion_entries = choices(
        &[
            ("mouse-exact", text("Точные клики", "Accurate clicks")),
            (
                "keyboard-gamepad",
                text(
                    "Разрешить произвольные искажения",
                    "Allow arbitrary distortion",
                ),
            ),
        ],
        Action::InputMode,
        c["input_mode"].as_str().unwrap(),
    );
    distortion_entries.extend([
        Entry {
            id: "model:distortion-help-devices".into(),
            label: text(
                "Клавиатура и мышь работают в обоих режимах",
                "Keyboard and mouse work in both modes",
            )
            .into(),
            enabled: false,
            checked: None,
        },
        Entry {
            id: "model:distortion-help-clicks".into(),
            label: text(
                "При произвольных искажениях клики могут смещаться",
                "Arbitrary distortion may misalign clicks",
            )
            .into(),
            enabled: false,
            checked: None,
        },
    ]);
    let settings = vec![
        group(
            text("Форма экрана", "Screen shape"),
            choices(
                &[
                    ("flat", text("Плоский", "Flat")),
                    ("rounded", text("Скруглённый CRT", "Rounded CRT")),
                    ("convex", text("Выпуклый CRT", "Curved CRT")),
                ],
                Action::Shape,
                c["screen"]["shape"].as_str().unwrap(),
            ),
        ),
        group(text("Формат 4:3", "4:3 format"), format_entries),
        group(
            text("Искажения изображения", "Image distortion"),
            distortion_entries,
        ),
        group(
            text("Масштаб", "Scaling"),
            choices(
                &[
                    ("fit", text("Вписать целиком", "Fit entire image")),
                    ("crop", text("Заполнить с обрезкой", "Fill and crop")),
                    ("stretch", text("Растянуть", "Stretch")),
                ],
                Action::Scale,
                c["aspect"]["scale"].as_str().unwrap(),
            ),
        ),
        group(
            text("Пропорции старых игр", "Legacy game aspect ratio"),
            dar_entries,
        ),
        group(
            text("Обработка", "Processing"),
            choices(
                &[
                    ("full-auto", text("Автоматически", "Automatic")),
                    ("full-gpu", text("Только GPU", "GPU only")),
                    (
                        "full-compatibility",
                        text(
                            "Режим совместимости CPU · до 30 FPS",
                            "CPU compatibility mode · up to 30 FPS",
                        ),
                    ),
                    (
                        "overlay",
                        text(
                            "Лёгкий режим · без цветовых шейдеров",
                            "Lightweight · no color shaders",
                        ),
                    ),
                ],
                Action::Backend,
                &backend,
            ),
        ),
        group(text("Горячие клавиши", "Hotkeys"), hotkeys),
    ];
    let phase = snapshot["runtime"]["phase"].as_str().unwrap();
    let status = match phase {
        "active"
            if snapshot["runtime"]["effect_active"] == true
                && snapshot["runtime"]["surface_visible"] == true =>
        {
            match snapshot["runtime"]["backend"].as_str().unwrap() {
                "full-gpu" => text("Работает · Full GPU", "Active · Full GPU"),
                "full-compatibility" => text("Работает · Full CPU", "Active · Full CPU"),
                "lightweight" => text("Работает · лёгкий режим", "Active · Lightweight"),
                _ => text("Ожидает изображения", "Waiting for image"),
            }
        }
        "active" => text("Ожидает изображения", "Waiting for image"),
        "disabled" => text("Эффект выключен", "Effect off"),
        "bypass" => text(
            "Исходное изображение · интенсивность 0%",
            "Original image · intensity 0%",
        ),
        "starting" => text("Подготовка", "Starting"),
        "ready" => text("Готов", "Ready"),
        "paused-settings" => text("Пауза · вернитесь в игру", "Paused · return to the game"),
        "paused-stale" => text("Захват приостановлен", "Capture paused"),
        "waiting-source" => text("Ожидает окно игры", "Waiting for game window"),
        "paused-focus" => text("Вернитесь в игру", "Return to the game"),
        "paused-moving" => text("Пауза при движении", "Paused while moving"),
        "waiting-frame" => text("Ожидает свежий кадр", "Waiting for a fresh frame"),
        "source-closed" => text("Окно игры закрыто", "Game window closed"),
        "error" => text("Ошибка", "Error"),
        "recovery-error" => text("Нужно восстановление", "Restoration required"),
        _ => unreachable!(),
    };
    let mut status = status.to_owned();
    if snapshot["runtime"]["format_active"] == true {
        status.push_str(text(" · формат 4:3 работает", " · 4:3 format active"));
    } else if c["aspect"]["enabled"] == true {
        status.push_str(text(
            " · формат 4:3 ожидает применения",
            " · 4:3 format pending",
        ));
    }
    if state.recovery && phase != "recovery-error" {
        status.push_str(text(" · нужно восстановление", " · restoration required"));
    }
    if snapshot["runtime"]["unsaved"] == true {
        status.push_str(text(" · настройки не сохранены", " · settings not saved"));
    }
    if state.confirmation {
        status.push_str(text(" · подтвердите видеорежим", " · confirm display mode"));
    }
    Ok(MenuModel {
        emergency_sequence: state.sequence,
        status,
        wants_off: state.should_stop,
        power: make(
            Action::Power,
            if state.should_stop {
                text("Выключить", "Turn off")
            } else {
                text("Включить", "Turn on")
            },
            None,
        ),
        sources: source_entries,
        effects,
        settings,
        confirmation_pending: state.confirmation,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    fn fixture() -> (Value, Value, Value) {
        let source = json!({"id":"window:0x1234:7:99","hwnd":"0x1234","pid":7,"process_created":"99","title":"Game"});
        let effects = json!({"intensity":0.3,"noise_seed":17,"freeze_noise":true,"crt":{"scanlines":0.12,"mask":0.04,"bloom":0.06,"softness":0.08,"vignette":0.10,"curvature":0.0},"vhs":{"chroma_bleed":0.0,"noise":0.0,"jitter":0.0,"tracking":0.0}});
        let config = json!({"version":1,"mode":"overlay","enabled":false,"capture":{"transfer":"gpu"},"target":{"kind":"window","monitor":0,"window_title":"Game"},"preset":"Subtle CRT","shader":{"id":"","params":[0,0,0,0,0,0,0,0]},"effects":effects,"screen":{"shape":"rounded","corner_radius":0.077,"curvature":0.18,"glass":0.21},"aspect":{"enabled":false,"method":"mask","scale":"fit","source_dar":0},"input_mode":"mouse-exact","hotkeys":{"toggle":"Ctrl+F8","emergency":"Ctrl+F10"}});
        let mut tape = effects.clone();
        tape["vhs"]["noise"] = json!(0.62);
        let snapshot = json!({"config":config,"source":source,"emergency_sequence":12,"confirmation_deadline":null,"presets":[{"name":"Subtle CRT","effects":effects},{"name":"VHS Tape","effects":tape},{"name":"Custom","effects":effects}],"runtime":{"phase":"disabled","backend":"","enabled":false,"effect_active":false,"surface_visible":false,"format_active":false,"recovery_pending":false,"unsaved":false,"gpu":{"state":"unavailable"},"compatibility":{"state":"unknown"}}});
        let sources = json!({"windows":[source],"monitors":[{"index":0}]});
        (snapshot, sources, json!({"items":[]}))
    }
    fn pack(id: char, name: &str, coordinates: &str) -> Value {
        json!({"id":id.to_string().repeat(64),"version":1,"name":name,"coordinates":coordinates,"parameters":[{"default":0.65}]})
    }
    #[test]
    fn encoded_ids_round_trip_without_titles_becoming_commands() {
        for name in [
            "Game",
            "A&B\tQuit\n",
            "🌈: model:off",
            "русское окно",
            "model:power",
        ] {
            let a = Action::Source(name.into());
            assert_eq!(parse_action(&a.id()), Some(a));
            let p = Action::Preset(name.into());
            assert_eq!(parse_action(&p.id()), Some(p));
        }
        for id in [
            "model:source:ff",
            "model:source:1",
            "model:source:AA",
            "model:source:",
            "model:monitor:01",
            "model:monitor:4294967296",
            "model:shader:abc",
            "model:format:delete",
            "power",
            "model:power:extra",
        ] {
            assert!(parse_action(id).is_none(), "{id}");
        }
        assert_eq!(
            menu_label("A&B\tFake shortcut\n\u{0}End"),
            "A&&B Fake shortcut  End"
        );
    }
    #[test]
    fn preset_changes_only_signal_and_promotes_legacy_window_to_auto() {
        let (s, windows, shaders) = fixture();
        let before = s.clone();
        let m = plan(&Action::Preset("VHS Tape".into()), &s, &windows, &shaders).unwrap();
        assert_eq!(m.kind, "apply");
        assert_eq!(m.payload["expected_config"], s["config"]);
        assert_eq!(m.payload["expected_emergency_sequence"], 12);
        assert_eq!(m.payload["source"], s["source"]);
        let c = &m.payload["config"];
        for key in [
            "enabled",
            "target",
            "screen",
            "aspect",
            "input_mode",
            "hotkeys",
        ] {
            assert_eq!(c[key], s["config"][key], "unexpected changed {key}");
        }
        assert_eq!(c["mode"], "full");
        assert_eq!(c["capture"]["transfer"], "auto");
        assert_eq!(c["effects"]["intensity"], 1);
        assert_eq!(c["effects"]["noise_seed"], 17);
        assert_eq!(c["effects"]["freeze_noise"], true);
        assert_eq!(s, before, "planning must not mutate the supplied snapshot");
    }
    #[test]
    fn source_selection_uses_identity_and_keeps_existing_preferences() {
        let (s, mut windows, shaders) = fixture();
        let other = json!({"id":"different","hwnd":"0xabcd","pid":8,"process_created":"100","title":"Game"});
        windows["windows"]
            .as_array_mut()
            .unwrap()
            .insert(0, other.clone());
        let menu = build(&s, &windows, &shaders).unwrap();
        assert_ne!(menu.sources[0].id, menu.sources[1].id);
        assert!(menu.sources[0].label.contains("PID 8"));
        assert_eq!(menu.sources[1].checked, Some(true));
        let action = parse_action(&menu.sources[0].id).unwrap();
        windows["windows"].as_array_mut().unwrap().reverse();
        let m = plan(&action, &s, &windows, &shaders).unwrap();
        assert_eq!(m.payload["source"], other);
        for key in [
            "enabled", "mode", "capture", "screen", "aspect", "effects", "hotkeys",
        ] {
            assert_eq!(m.payload["config"][key], s["config"][key]);
        }
        windows["windows"][1]["process_created"] = json!("101");
        assert!(plan(&action, &s, &windows, &shaders).is_err());
    }
    #[test]
    fn stop_restores_for_filter_format_recovery_unsaved_and_confirmation() {
        let (s, windows, shaders) = fixture();
        for pointer in [
            "/config/enabled",
            "/config/aspect/enabled",
            "/runtime/enabled",
            "/runtime/format_active",
            "/runtime/recovery_pending",
            "/runtime/unsaved",
        ] {
            let mut changed = s.clone();
            *changed.pointer_mut(pointer).unwrap() = json!(true);
            let menu = build(&changed, &windows, &shaders).unwrap();
            assert_eq!(menu.power.label, text("Выключить", "Turn off"), "{pointer}");
            assert!(menu.wants_off, "{pointer}");
            assert!(menu.power.enabled);
            let stop = plan(&Action::Power, &changed, &windows, &shaders).unwrap();
            assert_eq!(
                stop,
                Mutation {
                    kind: "emergency",
                    payload: Value::Null
                }
            );
        }
        let mut pending = s.clone();
        pending["confirmation_deadline"] = json!("2026-10-07T14:00:00Z");
        assert_eq!(
            plan(&Action::Power, &pending, &windows, &shaders)
                .unwrap()
                .kind,
            "emergency"
        );
        assert!(!build(&pending, &windows, &shaders).unwrap().settings[0].entries[0].enabled);
        assert_eq!(
            plan(&Action::Off, &Value::Null, &Value::Null, &Value::Null)
                .unwrap()
                .kind,
            "emergency"
        );
        let mut off = build(&s, &windows, &shaders).unwrap();
        assert!(!off.wants_off);
        off.power.label = "Turn on".into();
        assert!(!off.wants_off, "dispatch must not parse translated labels");
        let mut on = build(&pending, &windows, &shaders).unwrap();
        on.power.label = "Restore".into();
        assert!(on.wants_off, "dispatch must not parse translated labels");
    }
    #[test]
    fn source_is_never_replaced_by_another_same_title_window() {
        let (mut s, mut windows, shaders) = fixture();
        s["config"]["mode"] = json!("full");
        s["config"]["capture"]["transfer"] = json!("auto");
        windows["windows"][0]["hwnd"] = json!("0x5678");
        windows["windows"][0]["process_created"] = json!("999");
        let menu = build(&s, &windows, &shaders).unwrap();
        assert_eq!(menu.sources[0].checked, Some(false));
        assert!(!menu.power.enabled);
        assert!(plan(&Action::Power, &s, &windows, &shaders).is_err());
        s["source"] = Value::Null;
        assert!(plan(&Action::Power, &s, &windows, &shaders).is_err());
        let benign = plan(&Action::Hotkeys("compact".into()), &s, &windows, &shaders).unwrap();
        assert!(benign.payload.get("source").is_none());
        assert_eq!(benign.payload["config"]["target"], s["config"]["target"]);
    }
    #[test]
    fn invalid_or_unknown_snapshots_never_enable_menu_mutations() {
        let (s, windows, shaders) = fixture();
        for (pointer, value) in [
            ("/emergency_sequence", Value::Null),
            ("/emergency_sequence", json!(-1)),
            ("/runtime/phase", json!("future-phase")),
            ("/runtime/backend", json!("hardware")),
            ("/runtime/effect_active", Value::Null),
            ("/runtime/surface_visible", json!("true")),
            ("/config/enabled", json!("false")),
            ("/runtime/recovery_pending", Value::Null),
            ("/config/shader/params", json!([])),
            ("/runtime/gpu/state", json!("maybe")),
        ] {
            let mut bad = s.clone();
            *bad.pointer_mut(pointer).unwrap() = value;
            assert!(build(&bad, &windows, &shaders).is_err(), "{pointer}");
            assert!(
                plan(&Action::Power, &bad, &windows, &shaders).is_err(),
                "{pointer}"
            );
        }
    }
    #[test]
    fn custom_shader_names_are_disambiguated_and_reselection_preserves_values() {
        let (mut s, windows, _) = fixture();
        let shaders = json!({"items":[pack('a', "Game & glow\tBad", "preserve"),pack('b', "Game & glow\tBad", "preserve")]});
        let menu = build(&s, &windows, &shaders).unwrap();
        let custom: Vec<_> = menu
            .effects
            .iter()
            .filter(|e| e.id.starts_with("model:shader:"))
            .collect();
        assert_eq!(custom.len(), 2);
        assert_ne!(custom[0].id, custom[1].id);
        assert_ne!(custom[0].label, custom[1].label);
        assert!(custom[0].label.contains("&&"));
        let a = Action::Shader("a".repeat(64));
        let m = plan(&a, &s, &windows, &shaders).unwrap();
        assert_eq!(m.payload["config"]["shader"]["params"][0], 0.65);
        s["config"] = m.payload["config"].clone();
        s["config"]["shader"]["params"][0] = json!(0.42);
        assert_eq!(
            plan(&a, &s, &windows, &shaders).unwrap().payload["config"]["shader"]["params"][0],
            0.42
        );
    }
    #[test]
    fn warp_never_silently_changes_input_and_cannot_switch_back_to_exact() {
        let (mut s, windows, _) = fixture();
        let shaders = json!({"items":[pack('a', "Warp", "warp")]});
        let a = Action::Shader("a".repeat(64));
        assert!(plan(&a, &s, &windows, &shaders).is_err());
        s["config"]["input_mode"] = json!("keyboard-gamepad");
        let m = plan(&a, &s, &windows, &shaders).unwrap();
        s["config"] = m.payload["config"].clone();
        assert!(plan(
            &Action::InputMode("mouse-exact".into()),
            &s,
            &windows,
            &shaders
        )
        .is_err());
        assert!(plan(&Action::Backend("overlay".into()), &s, &windows, &shaders).is_err());
    }
    #[test]
    fn capability_and_geometry_rejections_precede_apply() {
        let (mut s, windows, shaders) = fixture();
        for action in [
            Action::Backend("full-gpu".into()),
            Action::Shape("convex".into()),
            Action::Scale("crop".into()),
            Action::Scale("stretch".into()),
            Action::Dar("4:3".into()),
        ] {
            assert!(plan(&action, &s, &windows, &shaders).is_err());
        }
        let mut c = plan(&Action::Backend("full-auto".into()), &s, &windows, &shaders)
            .unwrap()
            .payload["config"]
            .clone();
        c["input_mode"] = json!("keyboard-gamepad");
        s["config"] = c;
        assert!(plan(&Action::Shape("convex".into()), &s, &windows, &shaders).is_ok());
        assert!(plan(&Action::Dar("4:3".into()), &s, &windows, &shaders).is_ok());
        assert!(plan(&Action::Scale("crop".into()), &s, &windows, &shaders).is_ok());
    }
    #[test]
    fn unavailable_capture_does_not_block_hotkey_repair_or_preferences() {
        let (mut s, windows, shaders) = fixture();
        s["config"]["mode"] = json!("full");
        s["config"]["capture"]["transfer"] = json!("gpu");
        s["source"] = Value::Null;
        let empty = json!({"windows":[],"monitors":[]});
        for action in [
            Action::Hotkeys("compact".into()),
            Action::Shape("flat".into()),
            Action::Preset("VHS Tape".into()),
        ] {
            let mutation = plan(&action, &s, &empty, &shaders).unwrap();
            assert_eq!(mutation.payload["config"]["enabled"], false);
            assert_eq!(
                mutation.payload["config"]["capture"],
                s["config"]["capture"]
            );
            assert_eq!(mutation.payload["expected_config"], s["config"]);
        }
        assert!(plan(&Action::Backend("full-gpu".into()), &s, &windows, &shaders).is_err());
        assert!(plan(&Action::Format("mask".into()), &s, &windows, &shaders).is_err());
        assert!(plan(&Action::Power, &s, &windows, &shaders).is_err());
    }
    #[test]
    fn every_apply_keeps_cas_and_non_power_actions_do_not_reenable() {
        let (s, windows, shaders) = fixture();
        let menu = build(&s, &windows, &shaders).unwrap();
        let entries = menu
            .sources
            .iter()
            .chain(menu.effects.iter())
            .chain(menu.settings.iter().flat_map(|g| &g.entries));
        for entry in entries.filter(|e| e.enabled) {
            let action = parse_action(&entry.id).unwrap();
            let m = plan(&action, &s, &windows, &shaders).unwrap();
            assert_eq!(m.kind, "apply");
            assert_eq!(
                m.payload["expected_emergency_sequence"],
                s["emergency_sequence"]
            );
            assert_eq!(m.payload["expected_config"], s["config"]);
            assert_eq!(
                m.payload["config"]["enabled"], false,
                "{} must not enable filter",
                entry.id
            );
            if !matches!(action, Action::Hotkeys(_)) {
                assert_eq!(m.payload["config"]["hotkeys"], s["config"]["hotkeys"]);
            }
        }
        let mut after_emergency = s.clone();
        after_emergency["emergency_sequence"] = json!(13);
        let m = plan(
            &Action::Format("mask".into()),
            &after_emergency,
            &windows,
            &shaders,
        )
        .unwrap();
        assert_eq!(m.payload["expected_emergency_sequence"], 13);
        assert_eq!(m.payload["config"]["enabled"], false);
    }

    #[test]
    fn retired_formats_are_not_choices_but_legacy_profiles_can_be_restored() {
        for (method, label) in [
            (
                "mask",
                text("Маска 4:3 · из профиля", "4:3 mask · from profile"),
            ),
            (
                "window",
                text("Окно 4:3 · из профиля", "4:3 window · from profile"),
            ),
        ] {
            let (mut snapshot, sources, shaders) = fixture();
            let format = |model: MenuModel| {
                model
                    .settings
                    .into_iter()
                    .find(|group| group.label == text("Формат 4:3", "4:3 format"))
                    .unwrap()
            };
            let fresh = format(build(&snapshot, &sources, &shaders).unwrap());
            assert!(fresh.entries.iter().all(|entry| {
                entry.id != Action::Format(method.into()).id()
                    && entry.id != format!("model:format-profile-{method}")
            }));
            snapshot["config"]["aspect"]["enabled"] = json!(true);
            snapshot["config"]["aspect"]["method"] = json!(method);
            let before = snapshot.clone();
            let legacy = format(build(&snapshot, &sources, &shaders).unwrap());
            let selected: Vec<_> = legacy
                .entries
                .iter()
                .filter(|entry| entry.checked == Some(true))
                .collect();
            assert_eq!(selected.len(), 1);
            assert_eq!(selected[0].label, label);
            assert!(!selected[0].enabled);
            assert!(parse_action(&selected[0].id).is_none());
            assert!(legacy
                .entries
                .iter()
                .any(|entry| { entry.id == Action::Format("off".into()).id() && entry.enabled }));
            let off = plan(&Action::Format("off".into()), &snapshot, &sources, &shaders).unwrap();
            assert_eq!(off.payload["config"]["aspect"]["enabled"], false);
            assert_eq!(off.payload["config"]["aspect"]["method"], method);
            assert_eq!(snapshot, before, "viewing a profile must never migrate it");
            // Retiring a menu item must not remove the existing config contract.
            assert_eq!(
                parse_action(&Action::Format(method.into()).id()),
                Some(Action::Format(method.into()))
            );
        }
    }

    #[test]
    fn clearer_geometry_labels_retain_exact_mouse_restrictions() {
        let (mut snapshot, sources, shaders) = fixture();
        let model = build(&snapshot, &sources, &shaders).unwrap();
        assert_eq!(model.settings.len(), 7);
        let entry = |action: Action| {
            model
                .settings
                .iter()
                .flat_map(|group| &group.entries)
                .find(|entry| entry.id == action.id())
                .unwrap()
        };
        assert_eq!(
            entry(Action::InputMode("mouse-exact".into())).label,
            text("Точные клики", "Accurate clicks")
        );
        assert_eq!(
            entry(Action::Scale("fit".into())).label,
            text("Вписать целиком", "Fit entire image")
        );
        assert_eq!(
            entry(Action::Scale("crop".into())).label,
            text("Заполнить с обрезкой", "Fill and crop")
        );
        assert_eq!(
            entry(Action::Dar("auto".into())).label,
            text("Как в окне", "As in the window")
        );
        assert_eq!(
            entry(Action::Format("system".into())).label,
            text("Разрешение Windows…", "Windows resolution…")
        );
        assert!(model
            .settings
            .iter()
            .any(|group| group.label == text("Пропорции старых игр", "Legacy game aspect ratio")));
        assert!(entry(Action::Scale("fit".into())).enabled);
        for action in [
            Action::Scale("crop".into()),
            Action::Scale("stretch".into()),
            Action::Dar("4:3".into()),
            Action::Shape("convex".into()),
        ] {
            assert!(!entry(action.clone()).enabled);
            assert!(plan(&action, &snapshot, &sources, &shaders).is_err());
        }
        snapshot["config"]["mode"] = json!("full");
        snapshot["config"]["capture"]["transfer"] = json!("auto");
        snapshot["config"]["input_mode"] = json!("keyboard-gamepad");
        let crop = plan(&Action::Scale("crop".into()), &snapshot, &sources, &shaders).unwrap();
        snapshot["config"] = crop.payload["config"].clone();
        assert!(plan(
            &Action::InputMode("mouse-exact".into()),
            &snapshot,
            &sources,
            &shaders
        )
        .is_err());
    }
    #[test]
    fn builtin_convex_allows_mouse_without_changing_input_or_source() {
        let (mut snapshot, sources, shaders) = fixture();
        snapshot["config"]["mode"] = json!("full");
        snapshot["config"]["capture"]["transfer"] = json!("auto");
        let action = Action::Shape("convex".into());
        let model = build(&snapshot, &sources, &shaders).unwrap();
        let shape = model
            .settings
            .iter()
            .flat_map(|group| &group.entries)
            .find(|entry| entry.id == action.id())
            .unwrap();
        assert!(shape.enabled);
        assert_eq!(shape.label, text("Выпуклый CRT", "Curved CRT"));
        let next = plan(&action, &snapshot, &sources, &shaders).unwrap();
        assert_eq!(next.payload["config"]["screen"]["shape"], "convex");
        for field in ["input_mode", "target", "aspect", "hotkeys", "enabled"] {
            assert_eq!(next.payload["config"][field], snapshot["config"][field]);
        }
    }

    fn all_entries(model: &MenuModel) -> Vec<&Entry> {
        std::iter::once(&model.power)
            .chain(&model.sources)
            .chain(&model.effects)
            .chain(model.settings.iter().flat_map(|group| &group.entries))
            .collect()
    }

    #[test]
    fn whole_menu_translation_preserves_identity_state_external_names_and_profile() {
        use crate::i18n::{with_language, Language};

        let (base, mut sources, _) = fixture();
        let title = "Эффект выключен & Game\tWindow";
        sources["windows"][0]["title"] = json!(title);
        let shaders = json!({"items":[
            pack('a', "Точные клики & Glow", "preserve"),
            pack('b', "Turn off", "warp")
        ]});
        for scenario in 0..4 {
            let mut snapshot = base.clone();
            snapshot["source"]["title"] = json!(title);
            snapshot["config"]["target"]["window_title"] = json!(title);
            if scenario > 0 {
                snapshot["config"]["mode"] = json!("full");
                snapshot["config"]["capture"]["transfer"] = json!("auto");
                snapshot["config"]["input_mode"] = json!("keyboard-gamepad");
                snapshot["config"]["aspect"]["enabled"] = json!(true);
                snapshot["config"]["aspect"]["source_dar"] = json!(1.25);
                snapshot["runtime"]["phase"] = json!("active");
                snapshot["runtime"]["backend"] = json!("full-compatibility");
                snapshot["runtime"]["effect_active"] = json!(true);
                snapshot["runtime"]["surface_visible"] = json!(true);
                snapshot["runtime"]["format_active"] = json!(scenario == 1);
                snapshot["config"]["preset"] = json!("Custom");
            }
            if scenario == 2 {
                snapshot["config"]["shader"]["id"] = json!("a".repeat(64));
                snapshot["confirmation_deadline"] = json!("2026-10-08T14:00:00Z");
                snapshot["runtime"]["recovery_pending"] = json!(true);
                snapshot["runtime"]["unsaved"] = json!(true);
            }
            if scenario == 3 {
                // An unavailable saved asset is a translated system message,
                // unlike a shader author's unchanged display name.
                snapshot["config"]["shader"]["id"] = json!("c".repeat(64));
                snapshot["config"]["aspect"]["method"] = json!("window");
            }
            let before = (snapshot.clone(), sources.clone(), shaders.clone());
            let ru = with_language(Language::Ru, || {
                build(&snapshot, &sources, &shaders).unwrap()
            });
            let en = with_language(Language::En, || {
                build(&snapshot, &sources, &shaders).unwrap()
            });
            assert_eq!(ru.emergency_sequence, en.emergency_sequence);
            assert_eq!(ru.wants_off, en.wants_off);
            assert_eq!(ru.confirmation_pending, en.confirmation_pending);
            assert_ne!(ru.status, en.status);
            assert_ne!(ru.power.label, en.power.label);
            assert_eq!(ru.settings.len(), en.settings.len());
            for (ru_group, en_group) in ru.settings.iter().zip(&en.settings) {
                assert_ne!(ru_group.label, en_group.label);
                assert_eq!(ru_group.entries.len(), en_group.entries.len());
                assert!(!en_group
                    .label
                    .chars()
                    .any(|c| ('\u{0400}'..='\u{04ff}').contains(&c)));
            }
            assert_eq!(
                ru.sources, en.sources,
                "window titles must not be translated"
            );
            assert_eq!(en.sources[0].label, menu_label(title));
            let ru_entries = all_entries(&ru);
            let en_entries = all_entries(&en);
            assert_eq!(ru_entries.len(), en_entries.len());
            for (r, e) in ru_entries.into_iter().zip(en_entries) {
                assert_eq!((&r.id, r.enabled, r.checked), (&e.id, e.enabled, e.checked));
                let action = parse_action(&e.id);
                let external_name = matches!(
                    action.as_ref(),
                    Some(Action::Source(_) | Action::Shader(_))
                ) || matches!(action.as_ref(), Some(Action::Preset(name)) if name != "Custom");
                let binding = e.id.starts_with("model:hotkeys:") || e.id == "model:hotkeys-current";
                if external_name || binding {
                    assert_eq!(
                        r.label, e.label,
                        "external name or saved binding changed: {}",
                        e.id
                    );
                } else {
                    assert_ne!(
                        r.label, e.label,
                        "system label was not translated: {}",
                        e.id
                    );
                    assert!(
                        !e.label
                            .chars()
                            .any(|c| ('\u{0400}'..='\u{04ff}').contains(&c)),
                        "Russian leaked into English system label: {}",
                        e.label
                    );
                }
                if let Some(action) = action {
                    let ru_plan = with_language(Language::Ru, || {
                        plan(&action, &snapshot, &sources, &shaders)
                    });
                    let en_plan = with_language(Language::En, || {
                        plan(&action, &snapshot, &sources, &shaders)
                    });
                    assert_eq!(ru_plan.is_ok(), en_plan.is_ok());
                    if let (Ok(a), Ok(b)) = (ru_plan, en_plan) {
                        assert_eq!(a, b, "language changed the mutation or CAS: {}", e.id);
                    }
                }
            }
            for id in [
                "model:distortion-help-devices",
                "model:distortion-help-clicks",
            ] {
                let entry = all_entries(&en)
                    .into_iter()
                    .find(|entry| entry.id == id)
                    .unwrap();
                assert!(!entry.enabled);
                assert_eq!(entry.checked, None);
                assert!(parse_action(id).is_none());
            }
            assert_eq!(
                (snapshot, sources.clone(), shaders.clone()),
                before,
                "reading either language must not migrate a profile or rename an asset"
            );
        }
    }

    #[test]
    fn active_label_requires_a_visible_effect_not_just_an_enabled_request() {
        for language in [crate::i18n::Language::Ru, crate::i18n::Language::En] {
            crate::i18n::with_language(language, || {
                let (mut snapshot, sources, shaders) = fixture();
                snapshot["config"]["enabled"] = json!(true);
                snapshot["runtime"]["enabled"] = json!(true);
                snapshot["runtime"]["phase"] = json!("active");
                snapshot["runtime"]["backend"] = json!("full-compatibility");
                for (effect, visible) in [(false, false), (false, true), (true, false)] {
                    snapshot["runtime"]["effect_active"] = json!(effect);
                    snapshot["runtime"]["surface_visible"] = json!(visible);
                    let menu = build(&snapshot, &sources, &shaders).unwrap();
                    assert_eq!(
                        menu.status,
                        text("Ожидает изображения", "Waiting for image")
                    );
                    assert!(menu.wants_off, "Off must still cancel the enabled request");
                }
                snapshot["runtime"]["effect_active"] = json!(true);
                snapshot["runtime"]["surface_visible"] = json!(true);
                assert_eq!(
                    build(&snapshot, &sources, &shaders).unwrap().status,
                    text("Работает · Full CPU", "Active · Full CPU")
                );
                for phase in [
                    "disabled",
                    "bypass",
                    "paused-settings",
                    "paused-focus",
                    "waiting-frame",
                    "error",
                ] {
                    snapshot["runtime"]["phase"] = json!(phase);
                    let menu = build(&snapshot, &sources, &shaders).unwrap();
                    assert!(
                        !menu.status.starts_with(text("Работает", "Active")),
                        "{phase}"
                    );
                    assert!(menu.wants_off, "{phase}");
                }
            });
        }
    }

    #[test]
    fn every_runtime_phase_and_backend_has_english_status() {
        use crate::i18n::{with_language, Language};

        let (base, sources, shaders) = fixture();
        for (phase, backend) in [
            ("active", "full-gpu"),
            ("active", "full-compatibility"),
            ("active", "lightweight"),
            ("active", ""),
            ("disabled", ""),
            ("bypass", ""),
            ("starting", ""),
            ("ready", ""),
            ("paused-settings", ""),
            ("paused-stale", ""),
            ("waiting-source", ""),
            ("paused-focus", ""),
            ("paused-moving", ""),
            ("waiting-frame", ""),
            ("source-closed", ""),
            ("error", ""),
            ("recovery-error", ""),
        ] {
            let mut snapshot = base.clone();
            snapshot["runtime"]["phase"] = json!(phase);
            snapshot["runtime"]["backend"] = json!(backend);
            snapshot["runtime"]["effect_active"] = json!(phase == "active");
            snapshot["runtime"]["surface_visible"] = json!(phase == "active");
            let ru = with_language(Language::Ru, || {
                build(&snapshot, &sources, &shaders).unwrap()
            });
            let en = with_language(Language::En, || {
                build(&snapshot, &sources, &shaders).unwrap()
            });
            assert_ne!(ru.status, en.status, "{phase}/{backend}");
            assert!(!en.status.is_empty());
            assert!(
                !en.status
                    .chars()
                    .any(|c| ('\u{0400}'..='\u{04ff}').contains(&c)),
                "{}",
                en.status
            );
            assert_eq!(ru.wants_off, en.wants_off);
        }
    }

    #[test]
    fn localized_rejections_keep_field_details_and_never_change_config() {
        use crate::i18n::{with_language, Language};

        let (snapshot, sources, shaders) = fixture();
        let before = snapshot.clone();
        let action = Action::Scale("crop".into());
        let ru = with_language(Language::Ru, || {
            plan(&action, &snapshot, &sources, &shaders).unwrap_err()
        });
        let en = with_language(Language::En, || {
            plan(&action, &snapshot, &sources, &shaders).unwrap_err()
        });
        assert!(ru.contains("Разрешить произвольные искажения"));
        assert!(en.contains("Allow arbitrary distortion"));
        assert_eq!(snapshot, before);
        let field = "/runtime/phase";
        let ru_bad = with_language(Language::Ru, || bad(field));
        let en_bad = with_language(Language::En, || bad(field));
        assert_ne!(ru_bad, en_bad);
        assert!(ru_bad.contains(field));
        assert!(en_bad.contains(field));
        assert!(en_bad.contains("Wait a moment and reopen the menu."));
    }
}
