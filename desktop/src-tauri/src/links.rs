//! Native tray destinations. Never accept URLs or paths from shader content.
use tauri::AppHandle;
#[cfg(windows)]
use tauri_plugin_opener::OpenerExt;

pub fn open_repository(app: &AppHandle) -> Result<(), String> {
    #[cfg(windows)]
    {
        let app = app.clone();
        shell_action(move || {
            app.opener()
        .open_url("https://github.com/aiwaki/lumatape", None::<&str>)
        .map_err(|error| crate::localized!(
            "Не удалось открыть GitHub в браузере: {error}. Адрес: https://github.com/aiwaki/lumatape",
            "Could not open GitHub in your browser: {error}. Address: https://github.com/aiwaki/lumatape"
            ))
        })
    }
    #[cfg(not(windows))]
    {
        let _ = app;
        Err(crate::i18n::text(
            "Это действие доступно в Windows.",
            "This action is available on Windows.",
        )
        .into())
    }
}

pub fn open_effects_folder(app: &AppHandle) -> Result<(), String> {
    #[cfg(windows)]
    {
        // Match Go os.UserCacheDir(), including an isolated LOCALAPPDATA used
        // by smoke tests. KnownFolder APIs can resolve a different directory.
        let local = std::env::var_os("LOCALAPPDATA")
            .filter(|value| !value.is_empty())
            .map(std::path::PathBuf::from)
            .filter(|path| path.is_absolute())
            .ok_or_else(|| {
                crate::i18n::text(
                    "Windows не сообщил путь к папке эффектов.",
                    "Windows did not provide the effects folder location.",
                )
                .to_owned()
            })?;
        let directory = local.join("LumaTape").join("Shaders");
        std::fs::create_dir_all(&directory).map_err(|error| {
            crate::localized!(
                "Не удалось открыть папку эффектов: {error}",
                "Could not open the effects folder: {error}"
            )
        })?;
        let path = directory
            .to_str()
            .ok_or_else(|| {
                crate::i18n::text(
                    "Не удалось прочитать путь к папке эффектов.",
                    "Could not read the effects folder path.",
                )
                .to_owned()
            })?
            .to_owned();
        let app = app.clone();
        shell_action(move || {
            app.opener().open_path(path, None::<&str>).map_err(|error| {
                crate::localized!(
                    "Не удалось открыть папку эффектов в Проводнике: {error}",
                    "Could not open the effects folder in File Explorer: {error}"
                )
            })
        })
    }
    #[cfg(not(windows))]
    {
        let _ = app;
        Err(crate::i18n::text(
            "Папка эффектов доступна в Windows.",
            "The effects folder is available on Windows.",
        )
        .into())
    }
}

#[cfg(windows)]
fn shell_action(
    action: impl FnOnce() -> Result<(), String> + Send + 'static,
) -> Result<(), String> {
    // URL handlers can require STA COM. The opener's file path implementation
    // initializes COM, but its URL branch does not; don't inherit a pool thread.
    std::thread::Builder::new()
        .name("lumatape-open".into())
        .spawn(move || {
            #[link(name = "ole32")]
            extern "system" {
                fn CoInitializeEx(reserved: *mut std::ffi::c_void, flags: u32) -> i32;
                fn CoUninitialize();
            }
            let result = unsafe { CoInitializeEx(std::ptr::null_mut(), 0x2 | 0x4) };
            if result < 0 {
                return Err(crate::localized!(
                    "Не удалось открыть системное приложение (COM {result:#x}).",
                    "Could not open the system app (COM {result:#x})."
                ));
            }
            struct Apartment;
            impl Drop for Apartment {
                fn drop(&mut self) {
                    unsafe { CoUninitialize() };
                }
            }
            let _apartment = Apartment;
            action()
        })
        .map_err(|error| error.to_string())?
        .join()
        .map_err(|_| {
            crate::i18n::text(
                "Не удалось открыть системное приложение.",
                "Could not open the system app.",
            )
            .to_owned()
        })?
}
