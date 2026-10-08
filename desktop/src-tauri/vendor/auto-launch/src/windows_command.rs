//! LumaTape's local patch: always quote the Windows executable. Keep upstream
//! argument formatting unchanged; LumaTape registers no startup arguments.
pub(super) fn command(app_path: &str, args: &[String]) -> String {
    format!("\"{}\" {}", app_path, args.join(" "))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn quotes_spaces_and_unicode_without_losing_the_executable_path() {
        assert_eq!(
            command(r"C:\Users\Аня\Мои программы\LumaTape\lumatape.exe", &[]),
            "\"C:\\Users\\Аня\\Мои программы\\LumaTape\\lumatape.exe\" "
        );
    }

    #[test]
    fn preserves_upstream_argument_format() {
        assert_eq!(
            command(r"C:\Apps\app.exe", &["--one".into(), "--two".into()]),
            "\"C:\\Apps\\app.exe\" --one --two"
        );
    }
}
