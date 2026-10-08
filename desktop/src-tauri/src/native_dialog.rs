//! Native, synchronous tray actions. Call from a blocking task, never the tray event loop.
//! No WebView is created. The file picker owns a short-lived STA thread; clipboard
//! ownership uses an invisible message-only window on the calling thread.

use std::path::PathBuf;

pub fn show_message(title: &str, message: &str) -> Result<(), String> {
    platform::message_box(title, message, false, false).map(|_| ())
}

pub fn show_error(title: &str, message: &str) -> Result<(), String> {
    platform::message_box(title, message, true, false).map(|_| ())
}

/// The native Cancel button (including Escape/close) is the default selection.
pub fn confirm(title: &str, message: &str) -> Result<bool, String> {
    platform::message_box(title, message, false, true)
}

pub fn pick_shader_file() -> Result<Option<PathBuf>, String> {
    platform::pick_shader_file()
}

pub fn write_clipboard(text: &str) -> Result<(), String> {
    platform::write_clipboard(text)
}

/// Fixed project URL only; no configuration or shader content can supply a target.
pub fn open_repository() -> Result<(), String> {
    platform::open_repository()
}

#[cfg(any(windows, test))]
fn terminated_utf16(text: &str, limit: usize) -> Result<Vec<u16>, String> {
    let mut result = Vec::with_capacity(text.len().min(limit) + 1);
    for unit in text.encode_utf16() {
        if unit == 0 {
            return Err(crate::i18n::text(
                "native_text_invalid: текст содержит нулевой символ",
                "native_text_invalid: text contains a null character",
            )
            .into());
        }
        if result.len() == limit {
            return Err(crate::i18n::text(
                "native_text_too_long: текст превышает допустимый размер",
                "native_text_too_long: text exceeds the allowed length",
            )
            .into());
        }
        result.push(unit);
    }
    result.push(0);
    Ok(result)
}

#[cfg(any(windows, test))]
fn clipboard_utf16(text: &str) -> Result<Vec<u16>, String> {
    // CF_UNICODETEXT uses CRLF. Preserve existing CRLF instead of doubling CR.
    let units = terminated_utf16(text, 1_048_576)?;
    let mut result = Vec::with_capacity(units.len());
    let mut previous = 0;
    for unit in units {
        if unit == 10 && previous != 13 {
            result.push(13);
        }
        result.push(unit);
        previous = unit;
    }
    Ok(result)
}

#[cfg(not(windows))]
mod platform {
    use super::PathBuf;

    fn unavailable() -> String {
        crate::i18n::text(
            "native_unavailable: это действие доступно только в Windows",
            "native_unavailable: this action is available only on Windows",
        )
        .into()
    }
    pub(super) fn message_box(_: &str, _: &str, _: bool, _: bool) -> Result<bool, String> {
        Err(unavailable())
    }
    pub(super) fn pick_shader_file() -> Result<Option<PathBuf>, String> {
        Err(unavailable())
    }
    pub(super) fn write_clipboard(_: &str) -> Result<(), String> {
        Err(unavailable())
    }
    pub(super) fn open_repository() -> Result<(), String> {
        Err(unavailable())
    }
}

#[cfg(windows)]
mod platform {
    use super::{clipboard_utf16, terminated_utf16, PathBuf};
    use std::{
        ffi::{c_void, OsString},
        os::windows::ffi::OsStringExt,
        ptr,
    };

    type Handle = *mut c_void;
    type Hresult = i32;

    #[link(name = "shell32")]
    extern "system" {
        fn ShellExecuteW(
            owner: Handle,
            operation: *const u16,
            file: *const u16,
            parameters: *const u16,
            directory: *const u16,
            show: i32,
        ) -> isize;
    }

    pub(super) fn open_repository() -> Result<(), String> {
        // Shell extensions can require STA COM. Do not inherit the async pool's
        // apartment or block the native tray event loop.
        std::thread::Builder::new().name("lumatape-open-project".into()).spawn(|| {
            check_hr(unsafe { CoInitializeEx(ptr::null_mut(), 0x2 | 0x4) }, "com_initialize")?;
            let _apartment = Apartment;
            let operation = terminated_utf16("open", 8)?;
            let url = terminated_utf16("https://github.com/aiwaki/lumatape", 256)?;
            let result = unsafe { ShellExecuteW(ptr::null_mut(), operation.as_ptr(), url.as_ptr(), ptr::null(), ptr::null(), 1) };
            if result <= 32 {
                Err(crate::localized!("Не удалось открыть GitHub в браузере (код {result}). Адрес: https://github.com/aiwaki/lumatape", "Could not open GitHub in your browser (code {result}). Address: https://github.com/aiwaki/lumatape"))
            } else { Ok(()) }
        }).map_err(|error| error.to_string())?.join().map_err(|_| crate::i18n::text("Не удалось открыть страницу проекта.", "Could not open the project page.").to_owned())?
    }

    #[link(name = "user32")]
    extern "system" {
        fn MessageBoxW(owner: Handle, text: *const u16, title: *const u16, flags: u32) -> i32;
        fn CreateWindowExW(
            ex_style: u32,
            class: *const u16,
            title: *const u16,
            style: u32,
            x: i32,
            y: i32,
            width: i32,
            height: i32,
            parent: Handle,
            menu: Handle,
            instance: Handle,
            parameter: Handle,
        ) -> Handle;
        fn DestroyWindow(window: Handle) -> i32;
        fn OpenClipboard(owner: Handle) -> i32;
        fn CloseClipboard() -> i32;
        fn EmptyClipboard() -> i32;
        fn SetClipboardData(format: u32, data: Handle) -> Handle;
    }
    #[link(name = "kernel32")]
    extern "system" {
        fn GlobalAlloc(flags: u32, bytes: usize) -> Handle;
        fn GlobalFree(memory: Handle) -> Handle;
        fn GlobalLock(memory: Handle) -> Handle;
        fn GlobalUnlock(memory: Handle) -> i32;
        fn GetLastError() -> u32;
        fn SetLastError(code: u32);
    }
    #[link(name = "ole32")]
    extern "system" {
        fn CoInitializeEx(reserved: Handle, flags: u32) -> Hresult;
        fn CoUninitialize();
        fn CoCreateInstance(
            class: *const Guid,
            outer: Handle,
            context: u32,
            interface: *const Guid,
            object: *mut Handle,
        ) -> Hresult;
        fn CoTaskMemFree(memory: Handle);
    }

    fn win_error(operation: &str) -> String {
        format!("native_{operation}: Windows error {}", unsafe {
            GetLastError()
        })
    }
    fn check_hr(result: Hresult, operation: &str) -> Result<(), String> {
        if result < 0 {
            Err(format!(
                "native_{operation}: HRESULT 0x{:08X}",
                result as u32
            ))
        } else {
            Ok(())
        }
    }

    pub(super) fn message_box(
        title: &str,
        message: &str,
        error: bool,
        confirm: bool,
    ) -> Result<bool, String> {
        let title = terminated_utf16(title, 256)?;
        let message = terminated_utf16(message, 32_768)?;
        // MB_SETFOREGROUND; MB_OKCANCEL + MB_DEFBUTTON2 (Cancel); normal icons.
        let flags = 0x10000
            | if confirm {
                0x1 | 0x100 | 0x30
            } else if error {
                0x10
            } else {
                0x40
            };
        match unsafe { MessageBoxW(ptr::null_mut(), message.as_ptr(), title.as_ptr(), flags) } {
            0 => Err(win_error("message_box")),
            1 => Ok(true),
            2 => Ok(false),
            other => Err(crate::localized!(
                "native_message_box: неожиданный ответ {other}",
                "native_message_box: unexpected response {other}"
            )),
        }
    }

    struct Window(Handle);
    impl Drop for Window {
        fn drop(&mut self) {
            unsafe {
                DestroyWindow(self.0);
            }
        }
    }
    struct GlobalMemory(Handle);
    impl Drop for GlobalMemory {
        fn drop(&mut self) {
            if !self.0.is_null() {
                unsafe {
                    GlobalFree(self.0);
                }
            }
        }
    }
    struct Clipboard(bool);
    impl Drop for Clipboard {
        fn drop(&mut self) {
            if self.0 {
                unsafe {
                    CloseClipboard();
                }
            }
        }
    }

    pub(super) fn write_clipboard(text: &str) -> Result<(), String> {
        let text = clipboard_utf16(text)?;
        let class = terminated_utf16("STATIC", 6)?;
        // HWND_MESSAGE has no visible surface and never takes focus. OpenClipboard(NULL)
        // is intentionally avoided: EmptyClipboard would set a null clipboard owner.
        let owner = unsafe {
            CreateWindowExW(
                0,
                class.as_ptr(),
                ptr::null(),
                0,
                0,
                0,
                0,
                0,
                -3isize as Handle,
                ptr::null_mut(),
                ptr::null_mut(),
                ptr::null_mut(),
            )
        };
        if owner.is_null() {
            return Err(win_error("clipboard_owner"));
        }
        let _owner = Window(owner);
        // Allocate and fill before EmptyClipboard, so allocation failure leaves it intact.
        let mut memory = GlobalMemory(unsafe { GlobalAlloc(0x2, text.len() * size_of::<u16>()) });
        if memory.0.is_null() {
            return Err(win_error("clipboard_alloc"));
        }
        let destination = unsafe { GlobalLock(memory.0) };
        if destination.is_null() {
            return Err(win_error("clipboard_lock"));
        }
        unsafe {
            ptr::copy_nonoverlapping(text.as_ptr(), destination.cast::<u16>(), text.len());
            SetLastError(0);
            if GlobalUnlock(memory.0) == 0 && GetLastError() != 0 {
                return Err(win_error("clipboard_unlock"));
            }
        }
        let mut clipboard = Clipboard(false);
        // Other applications may briefly own the clipboard; never block the UI thread.
        for attempt in 0..10 {
            if unsafe { OpenClipboard(owner) } != 0 {
                clipboard.0 = true;
                break;
            }
            if attempt == 9 {
                return Err(win_error("clipboard_open"));
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        if unsafe { EmptyClipboard() } == 0 {
            return Err(win_error("clipboard_empty"));
        }
        if unsafe { SetClipboardData(13, memory.0) }.is_null() {
            return Err(win_error("clipboard_write"));
        }
        // Windows now owns the HGLOBAL and keeps this already-rendered text after
        // the message-only owner is destroyed. Never free it after transfer.
        memory.0 = ptr::null_mut();
        if unsafe { CloseClipboard() } == 0 {
            return Err(win_error("clipboard_close"));
        }
        clipboard.0 = false;
        Ok(())
    }

    #[repr(C)]
    struct Guid {
        data1: u32,
        data2: u16,
        data3: u16,
        data4: [u8; 8],
    }
    const FILE_OPEN_DIALOG: Guid = Guid {
        data1: 0xdc1c5a9c,
        data2: 0xe88a,
        data3: 0x4dde,
        data4: [0xa5, 0xa1, 0x60, 0xf8, 0x2a, 0x20, 0xae, 0xf7],
    };
    const I_FILE_OPEN_DIALOG: Guid = Guid {
        data1: 0xd57c7288,
        data2: 0xd4ad,
        data3: 0x4768,
        data4: [0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60],
    };
    #[repr(C)]
    struct Filter {
        name: *const u16,
        pattern: *const u16,
    }
    #[repr(C)]
    struct UnknownVtable {
        query_interface: usize,
        add_ref: usize,
        release: unsafe extern "system" fn(Handle) -> u32,
    }
    // Prefix of IFileOpenDialog's inherited IFileDialog vtable, through GetResult.
    // Unused entries are pointer-sized slots, matching the Windows SDK ABI.
    #[repr(C)]
    struct FileDialogVtable {
        unknown: UnknownVtable,
        show: unsafe extern "system" fn(Handle, Handle) -> Hresult,
        set_file_types: unsafe extern "system" fn(Handle, u32, *const Filter) -> Hresult,
        set_file_type_index: usize,
        get_file_type_index: usize,
        advise: usize,
        unadvise: usize,
        set_options: unsafe extern "system" fn(Handle, u32) -> Hresult,
        get_options: unsafe extern "system" fn(Handle, *mut u32) -> Hresult,
        set_default_folder: usize,
        set_folder: usize,
        get_folder: usize,
        get_current_selection: usize,
        set_file_name: usize,
        get_file_name: usize,
        set_title: unsafe extern "system" fn(Handle, *const u16) -> Hresult,
        set_ok_button_label: usize,
        set_file_name_label: usize,
        get_result: unsafe extern "system" fn(Handle, *mut Handle) -> Hresult,
    }
    #[repr(C)]
    struct ShellItemVtable {
        unknown: UnknownVtable,
        bind_to_handler: usize,
        get_parent: usize,
        get_display_name: unsafe extern "system" fn(Handle, u32, *mut *mut u16) -> Hresult,
    }
    struct ComObject(Handle);
    impl ComObject {
        unsafe fn vtable<T>(&self) -> &T {
            &**self.0.cast::<*const T>()
        }
    }
    impl Drop for ComObject {
        fn drop(&mut self) {
            unsafe {
                (self.vtable::<UnknownVtable>().release)(self.0);
            }
        }
    }
    struct Apartment;
    impl Drop for Apartment {
        fn drop(&mut self) {
            unsafe {
                CoUninitialize();
            }
        }
    }
    struct ComString(*mut u16);
    impl Drop for ComString {
        fn drop(&mut self) {
            unsafe {
                CoTaskMemFree(self.0.cast());
            }
        }
    }

    pub(super) fn pick_shader_file() -> Result<Option<PathBuf>, String> {
        // Do not assume a Tokio worker's COM apartment: use our own STA thread.
        std::thread::Builder::new()
            .name("lumatape-file-picker".into())
            .spawn(pick_on_sta)
            .map_err(|error| format!("native_picker_thread: {error}"))?
            .join()
            .map_err(|_| {
                crate::i18n::text(
                    "native_picker_thread: поток выбора файла завершился с ошибкой",
                    "native_picker_thread: the file picker thread failed",
                )
                .to_owned()
            })?
    }

    fn pick_on_sta() -> Result<Option<PathBuf>, String> {
        unsafe {
            // COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE.
            check_hr(CoInitializeEx(ptr::null_mut(), 0x2 | 0x4), "com_initialize")?;
            let _apartment = Apartment;
            let mut raw_dialog = ptr::null_mut();
            check_hr(
                CoCreateInstance(
                    &FILE_OPEN_DIALOG,
                    ptr::null_mut(),
                    1,
                    &I_FILE_OPEN_DIALOG,
                    &mut raw_dialog,
                ),
                "picker_create",
            )?;
            if raw_dialog.is_null() {
                return Err(crate::i18n::text(
                    "native_picker_create: пустой COM объект",
                    "native_picker_create: empty COM object",
                )
                .into());
            }
            let dialog = ComObject(raw_dialog);
            let table = dialog.vtable::<FileDialogVtable>();
            let title = terminated_utf16(
                crate::i18n::text("Добавить эффект LumaTape", "Add a LumaTape effect"),
                256,
            )?;
            let name = terminated_utf16(
                crate::i18n::text(
                    "Эффекты LumaTape (*.lumatape.glsl; *.glsl)",
                    "LumaTape effects (*.lumatape.glsl; *.glsl)",
                ),
                256,
            )?;
            let pattern = terminated_utf16("*.lumatape.glsl;*.glsl", 256)?;
            let filter = Filter {
                name: name.as_ptr(),
                pattern: pattern.as_ptr(),
            };
            check_hr(
                (table.set_file_types)(dialog.0, 1, &filter),
                "picker_filter",
            )?;
            check_hr((table.set_title)(dialog.0, title.as_ptr()), "picker_title")?;
            let mut options = 0;
            check_hr(
                (table.get_options)(dialog.0, &mut options),
                "picker_options",
            )?;
            // FOS_NOCHANGEDIR | FORCEFILESYSTEM | PATHMUSTEXIST | FILEMUSTEXIST |
            // DONTADDTORECENT; ensure single selection. Unlike OFN_NOCHANGEDIR,
            // this modern common-dialog flag actually preserves process CWD.
            options = (options | 0x8 | 0x40 | 0x800 | 0x1000 | 0x02000000) & !0x200;
            check_hr((table.set_options)(dialog.0, options), "picker_options")?;
            let result = (table.show)(dialog.0, ptr::null_mut());
            if result as u32 == 0x800704c7 {
                return Ok(None);
            } // ERROR_CANCELLED.
            check_hr(result, "picker_show")?;
            let mut raw_item = ptr::null_mut();
            check_hr((table.get_result)(dialog.0, &mut raw_item), "picker_result")?;
            if raw_item.is_null() {
                return Err(crate::i18n::text(
                    "native_picker_result: пустой COM объект",
                    "native_picker_result: empty COM object",
                )
                .into());
            }
            let item = ComObject(raw_item);
            let mut raw_path = ptr::null_mut();
            check_hr(
                (item.vtable::<ShellItemVtable>().get_display_name)(
                    item.0,
                    0x80058000,
                    &mut raw_path,
                ),
                "picker_path",
            )?; // SIGDN_FILESYSPATH.
            if raw_path.is_null() {
                return Err(crate::i18n::text(
                    "native_picker_path: пустой путь",
                    "native_picker_path: empty path",
                )
                .into());
            }
            let path = ComString(raw_path);
            // Shell returns a NUL-terminated allocated UTF-16 string. Bound the scan
            // to Windows' maximum Unicode path size; retain unpaired surrogates.
            let length =
                (0..32_768)
                    .find(|&index| *path.0.add(index) == 0)
                    .ok_or(crate::i18n::text(
                        "native_picker_path: путь превышает допустимый размер",
                        "native_picker_path: path exceeds the allowed length",
                    ))?;
            if length == 0 {
                return Err(crate::i18n::text(
                    "native_picker_path: пустой путь",
                    "native_picker_path: empty path",
                )
                .into());
            }
            let path = PathBuf::from(OsString::from_wide(std::slice::from_raw_parts(
                path.0, length,
            )));
            if !path
                .extension()
                .is_some_and(|extension| extension.eq_ignore_ascii_case("glsl"))
            {
                return Err(crate::i18n::text(
                    "native_picker_type: выберите файл .lumatape.glsl или .glsl",
                    "native_picker_type: select a .lumatape.glsl or .glsl file",
                )
                .into());
            }
            Ok(Some(path))
        }
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        #[test]
        fn com_vtable_prefixes_match_sdk_slot_offsets() {
            let slot = size_of::<usize>();
            assert_eq!(size_of::<Guid>(), 16);
            assert_eq!(std::mem::offset_of!(FileDialogVtable, show), 3 * slot);
            assert_eq!(
                std::mem::offset_of!(FileDialogVtable, set_options),
                9 * slot
            );
            assert_eq!(
                std::mem::offset_of!(FileDialogVtable, get_options),
                10 * slot
            );
            assert_eq!(std::mem::offset_of!(FileDialogVtable, set_title), 17 * slot);
            assert_eq!(
                std::mem::offset_of!(FileDialogVtable, get_result),
                20 * slot
            );
            assert_eq!(
                std::mem::offset_of!(ShellItemVtable, get_display_name),
                5 * slot
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn unicode_text_preserves_russian_and_surrogate_pairs() {
        let text = "Эфир 🎮";
        let encoded = terminated_utf16(text, 64).unwrap();
        assert_eq!(encoded.last(), Some(&0));
        assert_eq!(
            String::from_utf16(&encoded[..encoded.len() - 1]).unwrap(),
            text
        );
    }
    #[test]
    fn limits_count_utf16_units_and_never_truncate_text() {
        assert!(terminated_utf16("🎮", 1).is_err());
        assert_eq!(terminated_utf16("🎮", 2).unwrap().len(), 3);
        assert_eq!(terminated_utf16("", 0).unwrap(), vec![0]);
        assert!(terminated_utf16("text\0hidden", 64).is_err());
    }
    #[test]
    fn clipboard_normalizes_only_bare_line_feeds() {
        let encoded = clipboard_utf16("one\ntwo\r\nthree\r").unwrap();
        assert_eq!(
            String::from_utf16(&encoded[..encoded.len() - 1]).unwrap(),
            "one\r\ntwo\r\nthree\r"
        );
        assert!(clipboard_utf16("bad\0text").is_err());
    }
    #[cfg(not(windows))]
    #[test]
    fn unsupported_platform_is_never_reported_as_success_or_cancellation() {
        assert!(show_message("test", "test").is_err());
        assert!(show_error("test", "test").is_err());
        assert!(confirm("test", "test").is_err());
        assert!(pick_shader_file().is_err());
        assert!(write_clipboard("test").is_err());
    }
}
