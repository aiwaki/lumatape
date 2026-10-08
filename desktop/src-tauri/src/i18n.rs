//! Small native-shell locale boundary. Protocol keys and user content are never translated.
use std::sync::OnceLock;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Language {
    Ru,
    En,
}
impl Language {
    pub fn code(self) -> &'static str {
        match self {
            Self::Ru => "ru",
            Self::En => "en",
        }
    }
}
fn from_langid(id: u16) -> Language {
    if id & 0x03ff == 0x19 {
        Language::Ru
    } else {
        Language::En
    }
}
fn test_override(enabled: Option<&str>, value: Option<&str>) -> Option<Language> {
    if enabled != Some("1") {
        return None;
    }
    match value {
        Some("ru") => Some(Language::Ru),
        Some("en") => Some(Language::En),
        _ => None,
    }
}
fn detect() -> Language {
    // Only an explicit native test process can override the display language.
    // No settings are written to Windows or the application profile.
    if let Some(value) = test_override(
        std::env::var("LUMATAPE_NATIVE_UI_TEST").ok().as_deref(),
        std::env::var("LUMATAPE_TEST_LANGUAGE").ok().as_deref(),
    ) {
        return value;
    }
    #[cfg(windows)]
    {
        #[link(name = "kernel32")]
        extern "system" {
            fn GetUserDefaultUILanguage() -> u16;
        }
        // The UI language is independent of date/number formatting locale.
        from_langid(unsafe { GetUserDefaultUILanguage() })
    }
    #[cfg(not(windows))]
    Language::En
}
pub fn language() -> Language {
    #[cfg(test)]
    if let Some(value) = TEST_LANGUAGE.with(|current| current.get()) {
        return value;
    }
    static LANGUAGE: OnceLock<Language> = OnceLock::new();
    *LANGUAGE.get_or_init(detect)
}
pub fn is_ru() -> bool {
    language() == Language::Ru
}
pub fn text<'a>(ru: &'a str, en: &'a str) -> &'a str {
    if is_ru() {
        ru
    } else {
        en
    }
}

/// Each format is checked by Rust. Values (window titles, errors, paths) stay intact.
#[macro_export]
macro_rules! localized {
    ($ru:literal, $en:literal $(, $args:expr)* $(,)?) => {
        if $crate::i18n::is_ru() {
            format!($ru $(, $args)*)
        } else {
            format!($en $(, $args)*)
        }
    };
}

#[cfg(test)]
thread_local! {
    static TEST_LANGUAGE: std::cell::Cell<Option<Language>> = const { std::cell::Cell::new(None) };
}
#[cfg(test)]
pub fn with_language<T>(value: Language, operation: impl FnOnce() -> T) -> T {
    struct Restore(Option<Language>);
    impl Drop for Restore {
        fn drop(&mut self) {
            TEST_LANGUAGE.with(|current| current.set(self.0));
        }
    }
    let _restore = Restore(TEST_LANGUAGE.with(|current| current.replace(Some(value))));
    operation()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn windows_ui_languages_use_primary_language_and_english_fallback() {
        for id in [0x0419, 0x0819, 0x7c19] {
            assert_eq!(from_langid(id), Language::Ru);
        }
        for id in [0, 0x0409, 0x0809, 0x0407, 0x0422, 0x1000, 0x1400] {
            assert_eq!(from_langid(id), Language::En);
        }
        assert_eq!(test_override(None, Some("ru")), None);
        assert_eq!(test_override(Some("0"), Some("ru")), None);
        assert_eq!(test_override(Some("1"), Some("ru")), Some(Language::Ru));
        assert_eq!(test_override(Some("1"), Some("en")), Some(Language::En));
        assert_eq!(test_override(Some("1"), Some("invalid")), None);
    }
    #[test]
    fn scoped_languages_restore_even_on_panic_and_preserve_values() {
        let outer = language();
        with_language(Language::Ru, || {
            assert_eq!(text("Да", "Yes"), "Да");
            let title = "日本語 & Игра {text}";
            assert_eq!(
                crate::localized!("Окно: {title}", "Window: {title}"),
                format!("Окно: {title}")
            );
            let _ = std::panic::catch_unwind(|| with_language(Language::En, || panic!("test")));
            assert_eq!(language(), Language::Ru);
            with_language(Language::En, || assert_eq!(text("Да", "Yes"), "Yes"));
        });
        assert_eq!(language(), outer);
    }
}
