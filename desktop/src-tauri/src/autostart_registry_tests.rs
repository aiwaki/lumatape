//! Exercise the exact official plugin's patched Windows backend in a child
//! process whose HKCU is redirected. Never write to the actual login Run key.
use super::*;
use std::{process::Command, time::Duration};
use winreg::{enums::*, RegKey, RegValue};

const PREFIX: &str = r"Software\LumaTape.AutostartTests\";
const CHILD_KEY: &str = "LUMATAPE_AUTOSTART_TEST_HKCU";

#[test]
fn windows_backend_round_trip_uses_isolated_hkcu() {
    let name = format!(
        "{PREFIX}{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    );
    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    let (sandbox, disposition) = hkcu.create_subkey(&name).unwrap();
    // Never delete an existing key, even if a test-name collision occurred.
    assert_eq!(disposition, REG_CREATED_NEW_KEY);
    drop(sandbox);
    struct Cleanup(String);
    impl Drop for Cleanup {
        fn drop(&mut self) {
            let _ = RegKey::predef(HKEY_CURRENT_USER).delete_subkey_all(&self.0);
        }
    }
    let _cleanup = Cleanup(name.clone());
    let mut child = Command::new(std::env::current_exe().unwrap())
        .args([
            "--exact",
            "autostart::windows_tests::isolated_registry_child",
            "--ignored",
            "--nocapture",
            "--test-threads=1",
        ])
        .env(CHILD_KEY, name)
        .spawn()
        .unwrap();
    let deadline = std::time::Instant::now() + Duration::from_secs(30);
    loop {
        if let Some(status) = child.try_wait().unwrap() {
            assert!(status.success(), "isolated registry test: {status}");
            break;
        }
        if std::time::Instant::now() >= deadline {
            let _ = child.kill();
            let _ = child.wait();
            panic!("isolated registry test timed out");
        }
        std::thread::sleep(Duration::from_millis(20));
    }
}

#[test]
#[ignore = "Only invoked by the isolated child-process test above"]
fn isolated_registry_child() {
    let name = std::env::var(CHILD_KEY).expect("requires isolated parent test");
    assert!(name.starts_with(PREFIX));
    let sandbox = RegKey::predef(HKEY_CURRENT_USER)
        .open_subkey_with_flags(&name, KEY_ALL_ACCESS)
        .unwrap();
    #[link(name = "advapi32")]
    extern "system" {
        fn RegOverridePredefKey(key: isize, replacement: isize) -> i32;
    }
    assert_eq!(
        unsafe { RegOverridePredefKey(HKEY_CURRENT_USER as isize, sandbox.raw_handle() as isize) },
        0
    );
    struct Restore;
    impl Drop for Restore {
        fn drop(&mut self) {
            assert_eq!(
                unsafe { RegOverridePredefKey(HKEY_CURRENT_USER as isize, 0) },
                0
            );
        }
    }
    let _restore = Restore;
    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    let executable = r"C:\Users\Аня\Мои программы\LumaTape\lumatape.exe";
    let backend = auto_launch::AutoLaunch::new(ENTRY_NAME, executable, &[] as &[&str]);

    // A fresh account need not have either registry key yet.
    assert!(hkcu.open_subkey(windows_registry::RUN).is_err());
    backend.enable().unwrap();
    let run = hkcu.open_subkey(windows_registry::RUN).unwrap();
    assert_eq!(
        run.get_value::<String, _>(ENTRY_NAME).unwrap(),
        format!("\"{executable}\" ")
    );
    assert!(backend.is_enabled().unwrap());
    assert!(hkcu.open_subkey(windows_registry::APPROVED).is_err());

    // An explicit enable restores a Windows-disabled entry. A passive query
    // must never do so, including a disabled record with a zero timestamp.
    let (approved, _) = hkcu.create_subkey(windows_registry::APPROVED).unwrap();
    let mut bytes = vec![0; 12];
    bytes[0] = 3;
    approved
        .set_raw_value(
            ENTRY_NAME,
            &RegValue {
                vtype: REG_BINARY,
                bytes,
            },
        )
        .unwrap();
    assert!(!startup_approved(Some(
        &approved.get_raw_value(ENTRY_NAME).unwrap().bytes
    )));
    assert_eq!(approved.get_raw_value(ENTRY_NAME).unwrap().bytes[0], 3);
    backend.enable().unwrap();
    assert!(startup_approved(Some(
        &approved.get_raw_value(ENTRY_NAME).unwrap().bytes
    )));
    backend.disable().unwrap();
    assert_eq!(
        run.get_raw_value(ENTRY_NAME).unwrap_err().kind(),
        std::io::ErrorKind::NotFound
    );
    // Disable removes only Run, leaving Windows-owned approval metadata alone.
    assert!(approved.get_raw_value(ENTRY_NAME).is_ok());
    assert!(!backend.is_enabled().unwrap());
}
