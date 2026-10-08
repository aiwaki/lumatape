//! Windows-only installation boundary for the signed NSIS payload.
//! Exact packaging reference: Tauri CLI 2.11.3, windows/nsis/installer.nsi + utils.nsh.
//! Unlike plugin-updater 2.9.0's ShellExecuteW path, failed process creation is
//! returned to the live tray rather than followed by an unconditional exit.
use super::{fail, portable_reason, validate_installer, Failure, DOWNLOAD_LIMIT};
use std::{
    ffi::c_void,
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    os::windows::{
        ffi::OsStrExt,
        fs::{MetadataExt, OpenOptionsExt},
    },
    path::{Path, PathBuf},
    process::{Command, Stdio},
};
use tauri::Manager;

fn wide(value: &std::ffi::OsStr) -> Vec<u16> {
    value.encode_wide().chain(Some(0)).collect()
}
fn registry_string(key: &str, name: &str) -> Result<String, Failure> {
    #[link(name = "advapi32")]
    extern "system" {
        fn RegGetValueW(
            key: isize,
            subkey: *const u16,
            value: *const u16,
            flags: u32,
            kind: *mut u32,
            data: *mut c_void,
            bytes: *mut u32,
        ) -> i32;
    }
    let key = wide(key.as_ref());
    let name = wide(name.as_ref());
    let mut buffer = [0u16; 32768];
    let mut size = (buffer.len() * 2) as u32;
    // HKCU, REG_SZ only, 64-bit view used by the x64 NSIS installer.
    let result = unsafe {
        RegGetValueW(
            0x80000001u32 as i32 as isize,
            key.as_ptr(),
            name.as_ptr(),
            0x00000002 | 0x00010000,
            std::ptr::null_mut(),
            buffer.as_mut_ptr().cast(),
            &mut size,
        )
    };
    if result != 0 || size < 2 || size % 2 != 0 || size as usize > buffer.len() * 2 {
        return Err(portable_reason());
    }
    let count = size as usize / 2;
    if buffer[count - 1] != 0 || buffer[..count - 1].contains(&0) {
        return Err(portable_reason());
    }
    String::from_utf16(&buffer[..count - 1]).map_err(|_| portable_reason())
}
fn registered_path(value: &str) -> Result<PathBuf, Failure> {
    let value = if value.starts_with('"') && value.ends_with('"') {
        if value.len() < 2 {
            return Err(portable_reason());
        }
        &value[1..value.len() - 1]
    } else {
        value
    };
    if value.is_empty() || value.contains('"') {
        return Err(portable_reason());
    }
    let path = PathBuf::from(value);
    if !path.is_absolute() {
        return Err(portable_reason());
    }
    path.canonicalize().map_err(|_| portable_reason())
}
/// A folder copied from an installation is still portable: both NSIS registry
/// locations and the real running executable must identify the same directory.
pub(super) fn installed_directory() -> Result<PathBuf, Failure> {
    const KEY: &str = r"Software\Microsoft\Windows\CurrentVersion\Uninstall\LumaTape";
    if registry_string(KEY, "DisplayName")? != "LumaTape"
        || registry_string(KEY, "Publisher")? != "aiwaki"
        || registry_string(KEY, "MainBinaryName")? != "lumatape.exe"
        || registry_string(KEY, "DisplayVersion")? != env!("CARGO_PKG_VERSION")
    {
        return Err(portable_reason());
    }
    let registered = registered_path(&registry_string(KEY, "InstallLocation")?)?;
    let manufacturer = registered_path(&registry_string(r"Software\aiwaki\LumaTape", "")?)?;
    let current = std::env::current_exe()
        .and_then(|p| p.canonicalize())
        .map_err(|_| portable_reason())?;
    if registered != manufacturer
        || current.parent() != Some(registered.as_path())
        || current.file_name().and_then(|v| v.to_str()) != Some("lumatape.exe")
        || registered_path(&registry_string(KEY, "UninstallString")?)?
            != registered.join("uninstall.exe")
    {
        return Err(portable_reason());
    }
    Ok(registered)
}
fn disk_error() -> Failure {
    fail(
        "update_stage_failed",
        "Не удалось подготовить установщик на диске. Проверьте свободное место и повторите.",
        "Could not stage the installer. Check free disk space and retry.",
    )
}
fn regular_file(path: &Path) -> bool {
    fs::symlink_metadata(path).is_ok_and(|m| {
        m.is_file() && m.file_attributes() & 0x400 == 0 && m.len() <= DOWNLOAD_LIMIT as u64
    })
}
/// One known staging file, kept open without write/delete sharing until launch.
/// A failed attempt deletes it. A successful launch leaves exactly this file;
/// the next attempt can reclaim it only once Windows releases the executable.
pub(super) struct PreparedInstaller {
    path: PathBuf,
    locked: Option<File>,
    launched: bool,
}
impl PreparedInstaller {
    pub(super) fn prepare(
        app: &tauri::AppHandle,
        bytes: &[u8],
        version: &str,
    ) -> Result<Self, Failure> {
        validate_installer(bytes)?;
        let directory = app
            .path()
            .app_local_data_dir()
            .map_err(|_| disk_error())?
            .join("updates");
        Self::prepare_in(&directory, bytes, version)
    }
    fn prepare_in(directory: &Path, bytes: &[u8], version: &str) -> Result<Self, Failure> {
        validate_installer(bytes)?;
        fs::create_dir_all(directory).map_err(|_| disk_error())?;
        let metadata = fs::symlink_metadata(&directory).map_err(|_| disk_error())?;
        if !metadata.is_dir() || metadata.file_attributes() & 0x400 != 0 {
            return Err(disk_error());
        }
        let path = directory.join("pending-installer.exe");
        if path.try_exists().map_err(|_| disk_error())? {
            if !regular_file(&path) {
                return Err(disk_error());
            }
            fs::remove_file(&path).map_err(|_| {
                fail(
                    "update_stage_busy",
                    "Предыдущий установщик ещё занят. Дождитесь его завершения и повторите.",
                    "The previous installer is still in use. Wait for it to finish and retry.",
                )
            })?;
        }
        let mut prepared = Self {
            path,
            locked: None,
            launched: false,
        };
        let mut file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .share_mode(0)
            .open(&prepared.path)
            .map_err(|_| disk_error())?;
        file.write_all(bytes)
            .and_then(|_| file.sync_all())
            .map_err(|_| disk_error())?;
        drop(file);
        let mut locked = OpenOptions::new()
            .read(true)
            .share_mode(1)
            .open(&prepared.path)
            .map_err(|_| disk_error())?;
        let mut actual = Vec::with_capacity(bytes.len());
        Read::by_ref(&mut locked)
            .take(DOWNLOAD_LIMIT as u64 + 1)
            .read_to_end(&mut actual)
            .map_err(|_| disk_error())?;
        if actual != bytes {
            return Err(fail(
                "update_stage_changed",
                "Установщик изменился на диске. Обновление отклонено.",
                "The installer changed on disk. Update rejected.",
            ));
        }
        prepared.locked = Some(locked);
        validate_version(&prepared.path, version)?;
        Ok(prepared)
    }
    pub(super) fn launch(&mut self, directory: &Path) -> Result<(), Failure> {
        // Tauri's own passive/restart/update flags. No shell, no arbitrary
        // inherited command line, no /D override that could create another copy.
        Command::new(&self.path).args(["/P","/R","/UPDATE"])
            .current_dir(directory).stdin(Stdio::null()).stdout(Stdio::null()).stderr(Stdio::null())
            .spawn().map_err(|_|fail("update_launch_failed","Не удалось запустить установщик. Эффект выключен; перезапустите LumaTape и повторите.","Could not start the installer. The effect is off; restart LumaTape and retry."))?;
        self.launched = true;
        Ok(())
    }
}
impl Drop for PreparedInstaller {
    fn drop(&mut self) {
        self.locked.take();
        if !self.launched {
            let _ = fs::remove_file(&self.path);
        }
    }
}
/// Signature authenticates bytes; this independently binds the signed executable's
/// version resource to the (unsigned) manifest version, blocking renamed replays.
fn validate_version(path: &Path, expected: &str) -> Result<(), Failure> {
    #[link(name = "version")]
    extern "system" {
        fn GetFileVersionInfoSizeW(filename: *const u16, handle: *mut u32) -> u32;
        fn GetFileVersionInfoW(
            filename: *const u16,
            handle: u32,
            length: u32,
            data: *mut c_void,
        ) -> i32;
        fn VerQueryValueW(
            block: *const c_void,
            subblock: *const u16,
            buffer: *mut *mut c_void,
            len: *mut u32,
        ) -> i32;
    }
    let invalid = || {
        fail(
            "update_binary_version",
            "Версия подписанного установщика не совпадает с предложенным обновлением.",
            "The signed installer's version does not match the update offer.",
        )
    };
    let path = wide(path.as_os_str());
    let mut ignored = 0;
    let size = unsafe { GetFileVersionInfoSizeW(path.as_ptr(), &mut ignored) };
    if size < 52 || size > 1024 * 1024 {
        return Err(invalid());
    }
    let mut data = vec![0u8; size as usize];
    if unsafe { GetFileVersionInfoW(path.as_ptr(), 0, size, data.as_mut_ptr().cast()) } == 0 {
        return Err(invalid());
    }
    let mut pointer = std::ptr::null_mut();
    let mut len = 0;
    let root = wide("\\".as_ref());
    if unsafe { VerQueryValueW(data.as_ptr().cast(), root.as_ptr(), &mut pointer, &mut len) } == 0
        || pointer.is_null()
        || len < 52
    {
        return Err(invalid());
    }
    let start = pointer as usize;
    let base = data.as_ptr() as usize;
    if start < base
        || start
            .checked_add(52)
            .is_none_or(|end| end > base + data.len())
    {
        return Err(invalid());
    }
    // VS_FIXEDFILEINFO contains DWORD signature, version, file MS/LS, product MS/LS.
    let fields = unsafe { std::slice::from_raw_parts(pointer.cast::<u8>(), 52) };
    let word = |offset| u32::from_le_bytes(fields[offset..offset + 4].try_into().unwrap());
    let version = semver::Version::parse(expected).map_err(|_| invalid())?;
    let expected_parts = [version.major, version.minor, version.patch, 0];
    let matches = |ms: u32, ls: u32| {
        [
            u64::from(ms >> 16),
            u64::from(ms & 65535),
            u64::from(ls >> 16),
            u64::from(ls & 65535),
        ] == expected_parts
    };
    if word(0) != 0xfeef04bd || !matches(word(8), word(12)) || !matches(word(16), word(20)) {
        return Err(invalid());
    }
    Ok(())
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn malformed_registered_paths_are_rejected() {
        for value in ["", "relative", "\"C:\\x\" /args", "\"\"", "\""] {
            assert!(registered_path(value).is_err());
        }
    }
    fn scratch() -> PathBuf {
        static NEXT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
        let path = std::env::temp_dir().join(format!(
            "LumaTape-updater-test-{}-{}-{}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos(),
            NEXT.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
        ));
        fs::create_dir(&path).unwrap();
        path
    }
    #[test]
    fn signed_wrong_version_fixture_never_leaves_a_staged_executable() {
        use base64::Engine;
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../tests/fixtures/updater-signed.json")).unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(fixture["payload"].as_str().unwrap())
            .unwrap();
        let dir = scratch();
        let result = PreparedInstaller::prepare_in(&dir, &bytes, "0.3.2");
        assert_eq!(result.err().unwrap().code, "update_binary_version");
        assert!(!dir.join("pending-installer.exe").exists());
        fs::remove_dir(&dir).unwrap();
    }
    #[test]
    fn validated_staged_resource_stays_locked_until_cleanup() {
        use base64::Engine;
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../tests/fixtures/updater-signed.json")).unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(fixture["payload"].as_str().unwrap())
            .unwrap();
        let dir = scratch();
        let prepared = PreparedInstaller::prepare_in(&dir, &bytes, "0.3.1").unwrap();
        assert!(fs::remove_file(&prepared.path).is_err());
        assert!(OpenOptions::new().write(true).open(&prepared.path).is_err());
        let path = prepared.path.clone();
        drop(prepared);
        assert!(!path.exists());
        fs::remove_dir(dir).unwrap();
    }
    #[test]
    fn failed_create_process_does_not_mark_launch_success() {
        let dir = scratch();
        let path = dir.join("absent-installer.exe");
        let mut prepared = PreparedInstaller {
            path,
            locked: None,
            launched: false,
        };
        assert_eq!(
            prepared.launch(&dir).unwrap_err().code,
            "update_launch_failed"
        );
        assert!(!prepared.launched);
        drop(prepared);
        fs::remove_dir(dir).unwrap();
    }
    #[test]
    fn stage_does_not_remove_a_directory_at_its_owned_filename() {
        use base64::Engine;
        let fixture: serde_json::Value =
            serde_json::from_str(include_str!("../tests/fixtures/updater-signed.json")).unwrap();
        let bytes = base64::engine::general_purpose::STANDARD
            .decode(fixture["payload"].as_str().unwrap())
            .unwrap();
        let dir = scratch();
        let conflict = dir.join("pending-installer.exe");
        fs::create_dir(&conflict).unwrap();
        assert_eq!(
            PreparedInstaller::prepare_in(&dir, &bytes, "0.3.1")
                .err()
                .unwrap()
                .code,
            "update_stage_failed"
        );
        assert!(conflict.is_dir());
        fs::remove_dir(conflict).unwrap();
        fs::remove_dir(dir).unwrap();
    }
    #[test]
    fn portable_test_process_cannot_adopt_an_installed_copy() {
        assert_eq!(
            installed_directory().unwrap_err().code,
            "portable_install_disabled"
        );
    }
    #[test]
    fn arbitrary_binary_cannot_be_used_as_versioned_installer() {
        assert!(validate_version(&std::env::current_exe().unwrap(), "99.0.0").is_err());
    }
}
