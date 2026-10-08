#include "capture.h"

#include <windows.h>
#include <roapi.h>
#include <d3d11_4.h>
#include <dxgi1_6.h>
#include <dwmapi.h>
#include <GL/gl.h>
#include <windows.graphics.capture.interop.h>
#include <windows.graphics.directx.direct3d11.interop.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Metadata.h>
#include <winrt/Windows.Graphics.Capture.h>
#include <winrt/Windows.Graphics.DirectX.h>
#include <winrt/Windows.Graphics.DirectX.Direct3D11.h>

#include <algorithm>
#include <atomic>
#include <cstring>
#include <exception>
#include <memory>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

using winrt::com_ptr;
using namespace winrt::Windows::Graphics;
using namespace winrt::Windows::Graphics::Capture;
using namespace winrt::Windows::Graphics::DirectX;
using winrt::Windows::Graphics::DirectX::Direct3D11::IDirect3DDevice;
using winrt::Windows::Foundation::Metadata::ApiInformation;

namespace {
constexpr GLenum kReadOnly = 0x0000; // WGL_ACCESS_READ_ONLY_NV
constexpr GLenum kClampToEdge = 0x812F;
constexpr GLenum kRGBA8 = 0x8058, kBGRA = 0x80E1;
constexpr GLenum kPixelUnpackBuffer = 0x88EC, kPixelUnpackBufferBinding = 0x88EF;
constexpr int kBuffers = 2;

void require(bool ok, const char* message) {
    if (!ok) throw std::runtime_error(message);
}

void check_win32(BOOL ok, const char* what) {
    if (!ok) throw std::runtime_error(std::string(what) + " (Win32 " + std::to_string(GetLastError()) + ")");
}

void write_error(char* out, uint32_t capacity, std::string_view message) noexcept {
    if (!out || !capacity) return;
    const size_t count = std::min<size_t>(message.size(), capacity - 1);
    memcpy(out, message.data(), count);
    out[count] = '\0';
}

template <typename F>
int32_t boundary(char* error, uint32_t capacity, F&& action) noexcept {
    if (error && capacity) error[0] = '\0';
    try { return action(); }
    catch (const winrt::hresult_error& e) {
        try { write_error(error, capacity, winrt::to_string(e.message()) + " (HRESULT " + std::to_string(static_cast<int32_t>(e.code())) + ")"); }
        catch (...) { write_error(error, capacity, "Windows capture operation failed"); }
    }
    catch (const std::exception& e) { write_error(error, capacity, e.what()); }
    catch (...) { write_error(error, capacity, "Unknown Windows capture failure"); }
    return -1;
}

int64_t qpc_100ns() {
    LARGE_INTEGER counter{}, frequency{};
    check_win32(QueryPerformanceFrequency(&frequency), "QueryPerformanceFrequency");
    check_win32(QueryPerformanceCounter(&counter), "QueryPerformanceCounter");
    // Divide first: uptime * 10,000,000 can otherwise overflow int64.
    return (counter.QuadPart / frequency.QuadPart) * 10000000LL +
        (counter.QuadPart % frequency.QuadPart) * 10000000LL / frequency.QuadPart;
}

VhsCaptureTelemetryV1 fresh_telemetry() {
    VhsCaptureTelemetryV1 result{};
    result.struct_size = sizeof(result);
    result.version = 1;
    result.incoming_age_100ns = result.source_idle_100ns = -1;
    return result;
}

template <typename T>
T gl_proc(const char* name) {
    const auto p = wglGetProcAddress(name);
    if (!p || p == reinterpret_cast<PROC>(1) || p == reinterpret_cast<PROC>(2) ||
        p == reinterpret_cast<PROC>(3) || p == reinterpret_cast<PROC>(-1)) return nullptr;
    return reinterpret_cast<T>(p);
}

bool has_extension(std::string_view list, std::string_view required) {
    size_t begin = 0;
    while (begin < list.size()) {
        const size_t end = list.find(' ', begin);
        if (list.substr(begin, end == list.npos ? list.size() - begin : end - begin) == required) return true;
        if (end == list.npos) break;
        begin = end + 1;
    }
    return false;
}

struct Interop {
    HANDLE (WINAPI* open)(void*) = nullptr;
    BOOL (WINAPI* close)(HANDLE) = nullptr;
    HANDLE (WINAPI* register_object)(HANDLE, void*, GLuint, GLenum, GLenum) = nullptr;
    BOOL (WINAPI* unregister_object)(HANDLE, HANDLE) = nullptr;
    BOOL (WINAPI* lock)(HANDLE, GLint, HANDLE*) = nullptr;
    BOOL (WINAPI* unlock)(HANDLE, GLint, HANDLE*) = nullptr;

    void load() {
        using GetExtensionsARB = const char* (WINAPI*)(HDC);
        using GetExtensionsEXT = const char* (WINAPI*)();
        const auto arb = gl_proc<GetExtensionsARB>("wglGetExtensionsStringARB");
        const auto ext = gl_proc<GetExtensionsEXT>("wglGetExtensionsStringEXT");
        const char* extensions = arb ? arb(wglGetCurrentDC()) : (ext ? ext() : nullptr);
        require(extensions && has_extension(extensions, "WGL_NV_DX_interop") &&
            has_extension(extensions, "WGL_NV_DX_interop2"),
            "Full mode unavailable: this OpenGL driver lacks WGL_NV_DX_interop2; use Lightweight");
        open = gl_proc<decltype(open)>("wglDXOpenDeviceNV");
        close = gl_proc<decltype(close)>("wglDXCloseDeviceNV");
        register_object = gl_proc<decltype(register_object)>("wglDXRegisterObjectNV");
        unregister_object = gl_proc<decltype(unregister_object)>("wglDXUnregisterObjectNV");
        lock = gl_proc<decltype(lock)>("wglDXLockObjectsNV");
        unlock = gl_proc<decltype(unlock)>("wglDXUnlockObjectsNV");
        require(open && close && register_object && unregister_object && lock && unlock,
            "The driver advertises WGL interop but required entry points are missing");
    }
};

// A mapped D3D pointer must be interpreted as CPU memory, regardless of the
// renderer's pixel-unpack/PBO state. Restore every state value touched here.
struct UploadState {
    using BindBuffer = void (APIENTRY*)(GLenum, GLuint);
    BindBuffer bind_buffer = gl_proc<BindBuffer>("glBindBuffer");
    GLint texture = 0, buffer = 0, alignment = 0, row_length = 0, skip_rows = 0, skip_pixels = 0;
    UploadState(GLuint target, GLint row_pixels) {
        require(bind_buffer != nullptr, "OpenGL buffer entry point is unavailable");
        glGetIntegerv(GL_TEXTURE_BINDING_2D, &texture);
        glGetIntegerv(kPixelUnpackBufferBinding, &buffer);
        glGetIntegerv(GL_UNPACK_ALIGNMENT, &alignment);
        glGetIntegerv(GL_UNPACK_ROW_LENGTH, &row_length);
        glGetIntegerv(GL_UNPACK_SKIP_ROWS, &skip_rows);
        glGetIntegerv(GL_UNPACK_SKIP_PIXELS, &skip_pixels);
        bind_buffer(kPixelUnpackBuffer, 0);
        glBindTexture(GL_TEXTURE_2D, target);
        glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
        glPixelStorei(GL_UNPACK_ROW_LENGTH, row_pixels);
        glPixelStorei(GL_UNPACK_SKIP_ROWS, 0);
        glPixelStorei(GL_UNPACK_SKIP_PIXELS, 0);
    }
    ~UploadState() {
        glPixelStorei(GL_UNPACK_ALIGNMENT, alignment);
        glPixelStorei(GL_UNPACK_ROW_LENGTH, row_length);
        glPixelStorei(GL_UNPACK_SKIP_ROWS, skip_rows);
        glPixelStorei(GL_UNPACK_SKIP_PIXELS, skip_pixels);
        glBindTexture(GL_TEXTURE_2D, static_cast<GLuint>(texture));
        bind_buffer(kPixelUnpackBuffer, static_cast<GLuint>(buffer));
    }
};

struct Crop {
    RECT client{};
    UINT x = 0, y = 0, width = 0, height = 0;
};

bool get_client(HWND hwnd, RECT& out) {
    RECT client{};
    POINT origin{};
    if (!GetClientRect(hwnd, &client) || !ClientToScreen(hwnd, &origin)) return false;
    if (client.right <= 0 || client.bottom <= 0) return false;
    out = {origin.x, origin.y, origin.x + client.right, origin.y + client.bottom};
    return true;
}

// Window WGC includes the visible non-client frame. Only accept geometry that
// can be matched exactly, never guess offsets during DPI/resize transitions.
bool client_crop(HWND hwnd, SizeInt32 size, Crop& crop) {
    if (!get_client(hwnd, crop.client)) return false;
    const LONG width = crop.client.right - crop.client.left;
    const LONG height = crop.client.bottom - crop.client.top;
    LONG x = 0, y = 0;
    if (width != size.Width || height != size.Height) {
        RECT bounds{};
        if (FAILED(DwmGetWindowAttribute(hwnd, DWMWA_EXTENDED_FRAME_BOUNDS, &bounds, sizeof(bounds)))) return false;
        if (bounds.right - bounds.left != size.Width || bounds.bottom - bounds.top != size.Height) return false;
        x = crop.client.left - bounds.left;
        y = crop.client.top - bounds.top;
    }
    if (x < 0 || y < 0 || x + width > size.Width || y + height > size.Height) return false;
    crop.x = static_cast<UINT>(x);
    crop.y = static_cast<UINT>(y);
    crop.width = static_cast<UINT>(width);
    crop.height = static_cast<UINT>(height);
    return true;
}

struct MonitorNames {
    std::vector<std::wstring> names;
    bool failed = false;
};

BOOL CALLBACK collect_monitors(HMONITOR monitor, HDC, LPRECT, LPARAM data) {
    auto& result = *reinterpret_cast<MonitorNames*>(data);
    MONITORINFOEXW info{};
    info.cbSize = sizeof(info);
    if (!GetMonitorInfoW(monitor, &info)) { result.failed = true; return FALSE; }
    // A Win32 callback must never unwind a C++ exception through user32.
    try { result.names.emplace_back(info.szDevice); }
    catch (...) { result.failed = true; return FALSE; }
    return TRUE;
}

void require_sdr(const RECT& client) {
    MonitorNames monitors;
    require(EnumDisplayMonitors(nullptr, &client, collect_monitors, reinterpret_cast<LPARAM>(&monitors)) &&
        !monitors.failed && !monitors.names.empty(), "Cannot determine capture monitor color mode");
    std::vector<DISPLAYCONFIG_PATH_INFO> paths;
    std::vector<DISPLAYCONFIG_MODE_INFO> modes;
    LONG status = ERROR_INSUFFICIENT_BUFFER;
    // Display topology can change between the size query and QueryDisplayConfig.
    for (int attempt = 0; attempt < 3 && status == ERROR_INSUFFICIENT_BUFFER; ++attempt) {
        UINT32 path_count = 0, mode_count = 0;
        status = GetDisplayConfigBufferSizes(QDC_ONLY_ACTIVE_PATHS, &path_count, &mode_count);
        require(status == ERROR_SUCCESS, "Cannot query active display topology for HDR detection");
        paths.resize(path_count);
        modes.resize(mode_count);
        status = QueryDisplayConfig(QDC_ONLY_ACTIVE_PATHS, &path_count, paths.data(), &mode_count, modes.data(), nullptr);
        if (status == ERROR_SUCCESS) { paths.resize(path_count); modes.resize(mode_count); }
    }
    require(status == ERROR_SUCCESS, "Display topology changed during HDR detection; retry capture");
    for (const auto& name : monitors.names) {
        bool matched = false;
        for (const auto& path : paths) {
            DISPLAYCONFIG_SOURCE_DEVICE_NAME source{};
            source.header.type = DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME;
            source.header.size = sizeof(source);
            source.header.adapterId = path.sourceInfo.adapterId;
            source.header.id = path.sourceInfo.id;
            if (DisplayConfigGetDeviceInfo(&source.header) != ERROR_SUCCESS || name != source.viewGdiDeviceName) continue;
            matched = true;
            DISPLAYCONFIG_GET_ADVANCED_COLOR_INFO color{};
            color.header.type = DISPLAYCONFIG_DEVICE_INFO_GET_ADVANCED_COLOR_INFO;
            color.header.size = sizeof(color);
            color.header.adapterId = path.targetInfo.adapterId;
            color.header.id = path.targetInfo.id;
            require(DisplayConfigGetDeviceInfo(&color.header) == ERROR_SUCCESS,
                "Cannot verify SDR on the selected display; Full mode is unavailable");
            require(!color.advancedColorEnabled,
                "Full mode supports SDR only; HDR/Advanced Color is enabled on a source display");
        }
        require(matched, "Cannot match capture monitor to an active display color configuration");
    }
}
} // namespace

struct VhsCapture {
    DWORD thread_id = GetCurrentThreadId();
    HGLRC gl_context = wglGetCurrentContext();
    HWND hwnd = nullptr;
    DWORD target_pid = 0, target_thread = 0;
    bool apartment_initialized = false;
    uint32_t transfer = VHS_CAPTURE_TRANSFER_GPU;
    Interop interop;
    HANDLE interop_device = nullptr, interop_object = nullptr;
    GLuint texture = 0;
    bool locked = false, valid_frame = false;
    UINT texture_width = 0, texture_height = 0;
    com_ptr<ID3D11Device> device;
    com_ptr<ID3D11DeviceContext> context;
    com_ptr<ID3D11Texture2D> shared_texture;
    IDirect3DDevice rt_device{nullptr};
    GraphicsCaptureItem item{nullptr};
    Direct3D11CaptureFramePool pool{nullptr};
    GraphicsCaptureSession session{nullptr};
    std::shared_ptr<std::atomic_bool> source_closed = std::make_shared<std::atomic_bool>(false);
    winrt::event_token closed_token{};
    bool closed_subscribed = false;
    SizeInt32 pool_size{};
    Crop last_crop{};
    int64_t timestamp = 0;
    uint64_t serial = 0;
    RECT color_rect{};
    int64_t next_color_check = 0;
    int64_t crop_mismatch_since = 0;
    Direct3D11CaptureFrame pending_frame{nullptr};
    Crop pending_crop{};
    int64_t pending_copy_started = 0, pending_timestamp = 0;
    VhsCaptureStats stats{sizeof(VhsCaptureStats), VHS_CAPTURE_TRANSFER_GPU, 0, 0, 0, 0};
    VhsCaptureTelemetryV1 telemetry = fresh_telemetry();
    int64_t acquire_started = 0, last_source_received = 0;

    void begin_telemetry() {
        const auto sequence = telemetry.acquire_sequence + 1;
        const auto uploads = telemetry.uploads_total;
        telemetry = fresh_telemetry();
        telemetry.acquire_sequence = sequence;
        telemetry.uploads_total = uploads;
        acquire_started = qpc_100ns();
    }

    void end_telemetry() {
        const auto now = qpc_100ns();
        telemetry.acquire_call_100ns = std::max<int64_t>(0, now - acquire_started);
        telemetry.source_serial = serial;
        telemetry.pending_readback = pending_frame ? 1 : 0;
        if (pending_frame) telemetry.pending_age_100ns = std::max<int64_t>(0, now - pending_copy_started);
        if (last_source_received) telemetry.source_idle_100ns = std::max<int64_t>(0, now - last_source_received);
    }

    bool compatibility() const { return transfer == VHS_CAPTURE_TRANSFER_COMPATIBILITY; }

    void check_context() const {
        require(GetCurrentThreadId() == thread_id && wglGetCurrentContext() == gl_context && gl_context,
            "Capture must be used on its creating thread with the original OpenGL context current");
    }

    void unlock() {
        if (!locked) return;
        if (!compatibility()) check_win32(interop.unlock(interop_device, 1, &interop_object), "wglDXUnlockObjectsNV");
        locked = false;
    }

    void destroy_texture() {
        unlock();
        if (interop_object) {
            check_win32(interop.unregister_object(interop_device, interop_object), "wglDXUnregisterObjectNV");
            interop_object = nullptr;
        }
        if (texture) { glDeleteTextures(1, &texture); texture = 0; }
        shared_texture = nullptr;
        texture_width = texture_height = 0;
        valid_frame = false;
    }

    void create_texture(UINT width, UINT height) {
        if (width == texture_width && height == texture_height && texture && (compatibility() || interop_object)) return;
        destroy_texture();
        D3D11_TEXTURE2D_DESC description{};
        description.Width = width;
        description.Height = height;
        description.MipLevels = description.ArraySize = 1;
        description.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
        description.SampleDesc.Count = 1;
        description.Usage = compatibility() ? D3D11_USAGE_STAGING : D3D11_USAGE_DEFAULT;
        description.BindFlags = compatibility() ? 0 : D3D11_BIND_SHADER_RESOURCE | D3D11_BIND_RENDER_TARGET;
        description.CPUAccessFlags = compatibility() ? D3D11_CPU_ACCESS_READ : 0;
        // No CPU staging, keyed mutex or explicit shared handle is needed for
        // NV_DX_interop2 with an ID3D11Texture2D owned by this D3D device.
        winrt::check_hresult(device->CreateTexture2D(&description, nullptr, shared_texture.put()));
        glGenTextures(1, &texture);
        require(texture != 0, "OpenGL failed to allocate an interop texture name");
        if (compatibility()) {
            require(glGetError() == GL_NO_ERROR, "OpenGL has an error before compatibility texture allocation");
            {
                UploadState state(texture, 0);
                glTexImage2D(GL_TEXTURE_2D, 0, kRGBA8, width, height, 0, kBGRA, GL_UNSIGNED_BYTE, nullptr);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, kClampToEdge);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, kClampToEdge);
            }
            require(glGetError() == GL_NO_ERROR, "OpenGL compatibility texture allocation failed");
            texture_width = width;
            texture_height = height;
            return;
        }
        interop_object = interop.register_object(interop_device, shared_texture.get(), texture, GL_TEXTURE_2D, kReadOnly);
        require(interop_object != nullptr, "D3D11 BGRA texture registration with OpenGL failed; use Lightweight");
        check_win32(interop.lock(interop_device, 1, &interop_object), "Initial wglDXLockObjectsNV");
        locked = true;
        GLint old_texture = 0;
        glGetIntegerv(GL_TEXTURE_BINDING_2D, &old_texture);
        glBindTexture(GL_TEXTURE_2D, texture);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, kClampToEdge);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, kClampToEdge);
        GLint actual_width = 0, actual_height = 0;
        glGetTexLevelParameteriv(GL_TEXTURE_2D, 0, GL_TEXTURE_WIDTH, &actual_width);
        glGetTexLevelParameteriv(GL_TEXTURE_2D, 0, GL_TEXTURE_HEIGHT, &actual_height);
        glBindTexture(GL_TEXTURE_2D, static_cast<GLuint>(old_texture));
        unlock();
        require(actual_width == static_cast<GLint>(width) && actual_height == static_cast<GLint>(height),
            "OpenGL interop texture dimensions did not match the D3D11 resource");
        texture_width = width;
        texture_height = height;
    }

    void select_device() {
        com_ptr<IDXGIFactory1> factory;
        winrt::check_hresult(CreateDXGIFactory1(__uuidof(IDXGIFactory1), factory.put_void()));
        std::vector<com_ptr<IDXGIAdapter1>> adapters;
        const HMONITOR preferred_monitor = MonitorFromWindow(hwnd, MONITOR_DEFAULTTONEAREST);
        for (UINT index = 0;; ++index) {
            com_ptr<IDXGIAdapter1> adapter;
            const HRESULT result = factory->EnumAdapters1(index, adapter.put());
            if (result == DXGI_ERROR_NOT_FOUND) break;
            winrt::check_hresult(result);
            DXGI_ADAPTER_DESC1 desc{};
            winrt::check_hresult(adapter->GetDesc1(&desc));
            if (desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE) continue;
            bool preferred = false;
            for (UINT output_index = 0;; ++output_index) {
                com_ptr<IDXGIOutput> output;
                const HRESULT output_result = adapter->EnumOutputs(output_index, output.put());
                if (output_result == DXGI_ERROR_NOT_FOUND) break;
                winrt::check_hresult(output_result);
                DXGI_OUTPUT_DESC output_desc{};
                winrt::check_hresult(output->GetDesc(&output_desc));
                if (output_desc.Monitor == preferred_monitor) preferred = true;
            }
            if (preferred) adapters.insert(adapters.begin(), adapter);
            else adapters.push_back(adapter);
        }
        for (const auto& adapter : adapters) {
            const D3D_FEATURE_LEVEL levels[] = {D3D_FEATURE_LEVEL_11_0};
            const HRESULT result = D3D11CreateDevice(adapter.get(), D3D_DRIVER_TYPE_UNKNOWN, nullptr,
                D3D11_CREATE_DEVICE_BGRA_SUPPORT, levels, 1, D3D11_SDK_VERSION,
                device.put(), nullptr, context.put());
            if (FAILED(result)) { device = nullptr; context = nullptr; continue; }
            // WGC's internal worker and the host rendering thread share this
            // D3D device. Immediate-context access needs explicit protection.
            const auto multithread = context.as<ID3D11Multithread>();
            multithread->SetMultithreadProtected(TRUE);
            require(multithread->GetMultithreadProtected(), "D3D11 multithread protection is unavailable");
            if (!compatibility()) {
                interop_device = interop.open(device.get());
                if (!interop_device) { device = nullptr; context = nullptr; continue; }
            }
            try {
                // Opening a device alone is insufficient on hybrid-GPU systems:
                // actually register, lock and unlock the exact BGRA texture type.
                create_texture(2, 2);
                destroy_texture();
                return;
            } catch (...) {
                // If cleanup itself fails, do not continue with unsafe ownership.
                destroy_texture();
                if (interop_device) check_win32(interop.close(interop_device), "wglDXCloseDeviceNV after probe");
                interop_device = nullptr;
                context = nullptr;
                device = nullptr;
            }
        }
        throw std::runtime_error(compatibility() ?
            "No hardware D3D11 adapter supports the compatibility staging/GL texture path" :
            "No hardware D3D11 adapter can share BGRA textures with the current OpenGL context; use Lightweight");
    }

    void open(HWND target, uint32_t requested_transfer) {
        check_context();
        require(requested_transfer == VHS_CAPTURE_TRANSFER_GPU || requested_transfer == VHS_CAPTURE_TRANSFER_COMPATIBILITY,
            "Unknown capture transfer mode");
        transfer = requested_transfer;
        stats.transfer = requested_transfer;
        hwnd = target;
        require(IsWindow(hwnd) && GetAncestor(hwnd, GA_ROOT) == hwnd, "Select an existing top-level game window");
        target_thread = GetWindowThreadProcessId(hwnd, &target_pid);
        require(target_pid && target_pid != GetCurrentProcessId(), "The application cannot capture its own window");
        RECT client{};
        require(get_client(hwnd, client), "The selected game has no visible client area");
        require_sdr(client);
        color_rect = client;
        next_color_check = qpc_100ns() + 10000000LL;
        const HRESULT apartment = RoInitialize(RO_INIT_MULTITHREADED);
        if (apartment != RPC_E_CHANGED_MODE) {
            winrt::check_hresult(apartment);
            apartment_initialized = true;
        }
        require(GraphicsCaptureSession::IsSupported(), "Windows Graphics Capture is unavailable on this system");
        require(ApiInformation::IsPropertyPresent(L"Windows.Graphics.Capture.GraphicsCaptureSession", L"IsCursorCaptureEnabled"),
            "Full mode requires Windows 10 version 2004 (build 19041) or newer to avoid a double cursor");
        if (!compatibility()) interop.load();
        select_device();
        const auto dxgi_device = device.as<IDXGIDevice>();
        com_ptr<IInspectable> inspectable;
        winrt::check_hresult(CreateDirect3D11DeviceFromDXGIDevice(dxgi_device.get(), inspectable.put()));
        rt_device = inspectable.as<IDirect3DDevice>();
        const auto factory = winrt::get_activation_factory<GraphicsCaptureItem, IGraphicsCaptureItemInterop>();
        winrt::check_hresult(factory->CreateForWindow(hwnd, winrt::guid_of<GraphicsCaptureItem>(), winrt::put_abi(item)));
        const auto closed = source_closed;
        closed_token = item.Closed([closed](auto const&, auto const&) { closed->store(true); });
        closed_subscribed = true;
        pool_size = item.Size();
        require(pool_size.Width > 0 && pool_size.Height > 0, "Capture source has an empty size");
        pool = Direct3D11CaptureFramePool::CreateFreeThreaded(rt_device, DirectXPixelFormat::B8G8R8A8UIntNormalized, kBuffers, pool_size);
        session = pool.CreateCaptureSession(item);
        session.IsCursorCaptureEnabled(false);
        require(!session.IsCursorCaptureEnabled(), "Windows did not disable cursor capture");
        session.StartCapture();
    }

    bool finish_readback(const RECT& current_client) {
        if (!pending_frame) return false;
        D3D11_MAPPED_SUBRESOURCE mapped{};
        const int64_t map_started = qpc_100ns();
        const HRESULT result = context->Map(shared_texture.get(), 0, D3D11_MAP_READ,
            D3D11_MAP_FLAG_DO_NOT_WAIT, &mapped);
        telemetry.map_call_100ns += std::max<int64_t>(0, qpc_100ns() - map_started);
        if (result == DXGI_ERROR_WAS_STILL_DRAWING) {
            winrt::check_hresult(device->GetDeviceRemovedReason());
            require(map_started - pending_copy_started < 20000000LL, "Compatibility readback stalled for two seconds");
            return false;
        }
        winrt::check_hresult(result);
        const bool geometry_matches = current_client.right - current_client.left == static_cast<LONG>(texture_width) &&
            current_client.bottom - current_client.top == static_cast<LONG>(texture_height);
        int64_t map_ready = 0, upload_elapsed = 0;
        {
            struct Unmap {
                ID3D11DeviceContext* context;
                ID3D11Texture2D* texture;
                int64_t* elapsed;
                void finish() {
                    const auto started = qpc_100ns();
                    context->Unmap(texture, 0);
                    context = nullptr;
                    *elapsed += std::max<int64_t>(0, qpc_100ns() - started);
                }
                ~Unmap() { if (context) context->Unmap(texture, 0); }
            } unmap{context.get(), shared_texture.get(), &telemetry.unmap_call_100ns};
            map_ready = qpc_100ns();
            require(mapped.pData && mapped.RowPitch >= texture_width * 4 && mapped.RowPitch % 4 == 0,
                "Unexpected compatibility readback row pitch");
            if (geometry_matches) {
                const int64_t prepare_started = qpc_100ns();
                require(glGetError() == GL_NO_ERROR, "OpenGL has an error before compatibility upload");
                int64_t restore_started = 0;
                {
                    UploadState state(texture, static_cast<GLint>(mapped.RowPitch / 4));
                    const int64_t upload_started = qpc_100ns();
                    telemetry.gl_prepare_100ns += std::max<int64_t>(0, upload_started - prepare_started);
                    glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, texture_width, texture_height,
                        kBGRA, GL_UNSIGNED_BYTE, mapped.pData);
                    upload_elapsed = qpc_100ns() - upload_started;
                    telemetry.upload_call_100ns += std::max<int64_t>(0, upload_elapsed);
                    ++telemetry.uploads_this_acquire;
                    ++telemetry.uploads_total;
                    restore_started = qpc_100ns();
                }
                require(glGetError() == GL_NO_ERROR, "OpenGL compatibility upload failed");
                telemetry.gl_restore_100ns += std::max<int64_t>(0, qpc_100ns() - restore_started);
            }
            unmap.finish();
        }
        const int64_t transfer_elapsed = qpc_100ns() - map_started;
        // Map ready proves the GPU copy completed. Only now return its source
        // buffer to WGC; the uploaded GL texture no longer depends on that frame.
        const auto close_started = qpc_100ns();
        pending_frame.Close();
        pending_frame = nullptr;
        telemetry.frame_close_100ns += std::max<int64_t>(0, qpc_100ns() - close_started);
        if (!geometry_matches) { valid_frame = false; return false; }
        stats = {sizeof(stats), transfer, std::max<int64_t>(0, map_ready - pending_copy_started),
            std::max<int64_t>(0, upload_elapsed), std::max<int64_t>(0, transfer_elapsed),
            static_cast<uint64_t>(texture_width) * texture_height * 4};
        timestamp = pending_timestamp;
        last_crop = pending_crop;
        valid_frame = true;
        ++serial;
        return true;
    }

    int32_t acquire_compatibility(VhsCaptureFrame& out, const RECT& current_client, int64_t now) {
        bool updated = finish_readback(current_client);
        uint32_t dropped = 0;
        // Exactly one readback may be in flight. A busy Map returns immediately;
        // the two-buffer WGC pool is the only upstream queue, with bounded drain.
        if (!pending_frame) {
            Direct3D11CaptureFrame latest{nullptr};
            for (int i = 0; i < kBuffers; ++i) {
                auto frame = pool.TryGetNextFrame();
                if (!frame) break;
                if (latest) { latest.Close(); ++dropped; }
                latest = std::move(frame);
            }
            if (latest) {
                last_source_received = qpc_100ns();
                telemetry.incoming_age_100ns = std::max<int64_t>(0,
                    last_source_received - latest.SystemRelativeTime().count());
                const auto size = latest.ContentSize();
                if (size.Width <= 0 || size.Height <= 0) { latest.Close(); valid_frame = false; return 0; }
                if (size.Width != pool_size.Width || size.Height != pool_size.Height) {
                    latest.Close();
                    latest = nullptr;
                    valid_frame = false;
                    pool.Recreate(rt_device, DirectXPixelFormat::B8G8R8A8UIntNormalized, kBuffers, size);
                    pool_size = size;
                    return 0;
                }
                Crop crop{};
                if (!client_crop(hwnd, size, crop)) {
                    latest.Close();
                    valid_frame = false;
                    if (!crop_mismatch_since) crop_mismatch_since = now;
                    require(now - crop_mismatch_since < 20000000LL,
                        "Cannot align captured content with the game client area; use borderless mode or Lightweight");
                    return 0;
                }
                crop_mismatch_since = 0;
                const auto access = latest.Surface().as<::Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess>();
                com_ptr<ID3D11Texture2D> source;
                winrt::check_hresult(access->GetInterface(__uuidof(ID3D11Texture2D), source.put_void()));
                D3D11_TEXTURE2D_DESC source_desc{};
                source->GetDesc(&source_desc);
                require(source_desc.Format == DXGI_FORMAT_B8G8R8A8_UNORM && source_desc.SampleDesc.Count == 1 &&
                    crop.x + crop.width <= source_desc.Width && crop.y + crop.height <= source_desc.Height,
                    "Capture returned an unexpected texture format or size");
                create_texture(crop.width, crop.height);
                const D3D11_BOX box{crop.x, crop.y, 0, crop.x + crop.width, crop.y + crop.height, 1};
                pending_copy_started = qpc_100ns();
                pending_timestamp = latest.SystemRelativeTime().count();
                pending_crop = crop;
                pending_frame = std::move(latest);
                context->CopySubresourceRegion(shared_texture.get(), 0, 0, 0, 0, source.get(), 0, &box);
                const auto flush_started = qpc_100ns();
                telemetry.copy_submit_100ns += std::max<int64_t>(0, flush_started - pending_copy_started);
                context->Flush();
                telemetry.flush_call_100ns += std::max<int64_t>(0, qpc_100ns() - flush_started);
                winrt::check_hresult(device->GetDeviceRemovedReason());
                // A completed entry readback has already uploaded this acquire's
                // image. Keep the next copy in flight instead of immediately
                // uploading a second image that overwrites an unpresented first
                // one. Startup can still complete its first copy without delay.
                if (!updated) updated = finish_readback(current_client);
            }
        }
        if (!valid_frame) return 0;
        if (current_client.right - current_client.left != static_cast<LONG>(texture_width) ||
            current_client.bottom - current_client.top != static_cast<LONG>(texture_height)) {
            valid_frame = false;
            return 0;
        }
        // This is a logical ownership guard; only GPU mode takes an NV lock.
        locked = true;
        last_crop.client = current_client;
        out = {sizeof(out), texture, texture_width, texture_height,
            last_crop.client.left, last_crop.client.top, updated ? 1U : 0U, dropped,
            timestamp, std::max<int64_t>(0, qpc_100ns() - timestamp), serial};
        return 1;
    }

    int32_t acquire(VhsCaptureFrame& out) {
        check_context();
        require(!locked, "Release the previous capture texture before acquiring another frame");
        DWORD pid = 0;
        const DWORD target_tid = GetWindowThreadProcessId(hwnd, &pid);
        require(!source_closed->load() && IsWindow(hwnd) && pid == target_pid && target_tid == target_thread,
            "The captured game window has closed");
        if (IsIconic(hwnd) || !IsWindowVisible(hwnd)) { valid_frame = false; return 0; }
        RECT current_client{};
        if (!get_client(hwnd, current_client)) { valid_frame = false; return 0; }
        const int64_t now = qpc_100ns();
        if (!EqualRect(&current_client, &color_rect) || now >= next_color_check) {
            require_sdr(current_client);
            color_rect = current_client;
            next_color_check = now + 10000000LL;
        }
        if (compatibility()) return acquire_compatibility(out, current_client, now);
        uint32_t dropped = 0;
        Direct3D11CaptureFrame latest{nullptr};
        // Bounded drain: there are only two capture buffers. No callback queue,
        // unbounded polling, frame wait, CPU readback or busy loop.
        for (int i = 0; i < kBuffers; ++i) {
            auto frame = pool.TryGetNextFrame();
            if (!frame) break;
            if (latest) { latest.Close(); ++dropped; }
            latest = std::move(frame);
        }
        bool updated = false;
        if (latest) {
            const auto size = latest.ContentSize();
            if (size.Width <= 0 || size.Height <= 0) { latest.Close(); valid_frame = false; return 0; }
            if (size.Width != pool_size.Width || size.Height != pool_size.Height) {
                // Growing content is clipped by the old pool; shrinking content
                // leaves undefined margins. Discard both and wait for a fresh size.
                latest.Close();
                latest = nullptr;
                valid_frame = false;
                pool.Recreate(rt_device, DirectXPixelFormat::B8G8R8A8UIntNormalized, kBuffers, size);
                pool_size = size;
                return 0;
            }
            Crop crop{};
            if (!client_crop(hwnd, size, crop)) {
                latest.Close();
                valid_frame = false;
                if (!crop_mismatch_since) crop_mismatch_since = now;
                require(now - crop_mismatch_since < 20000000LL,
                    "Cannot align captured content with the game client area; use borderless mode or Lightweight");
                return 0;
            }
            crop_mismatch_since = 0;
            const auto access = latest.Surface().as<::Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess>();
            com_ptr<ID3D11Texture2D> source;
            winrt::check_hresult(access->GetInterface(__uuidof(ID3D11Texture2D), source.put_void()));
            D3D11_TEXTURE2D_DESC source_desc{};
            source->GetDesc(&source_desc);
            require(source_desc.Format == DXGI_FORMAT_B8G8R8A8_UNORM && source_desc.SampleDesc.Count == 1 &&
                crop.x + crop.width <= source_desc.Width && crop.y + crop.height <= source_desc.Height,
                "Capture returned an unexpected texture format or size");
            create_texture(crop.width, crop.height);
            const D3D11_BOX box{crop.x, crop.y, 0, crop.x + crop.width, crop.y + crop.height, 1};
            context->CopySubresourceRegion(shared_texture.get(), 0, 0, 0, 0, source.get(), 0, &box);
            context->Flush();
            winrt::check_hresult(device->GetDeviceRemovedReason());
            // WGL lock transfers ownership and synchronizes the GPU copy before
            // GL samples it. Keep the WGC frame alive until that handoff completes.
            check_win32(interop.lock(interop_device, 1, &interop_object), "wglDXLockObjectsNV");
            locked = true;
            timestamp = latest.SystemRelativeTime().count();
            source = nullptr;
            latest.Close();
            valid_frame = true;
            last_crop = crop;
            ++serial;
            updated = true;
        } else {
            if (!valid_frame) return 0;
            // Never stretch a previous frame into newly resized source geometry.
            if (current_client.right - current_client.left != static_cast<LONG>(texture_width) ||
                current_client.bottom - current_client.top != static_cast<LONG>(texture_height)) {
                valid_frame = false;
                return 0;
            }
            check_win32(interop.lock(interop_device, 1, &interop_object), "wglDXLockObjectsNV (repeat frame)");
            locked = true;
            last_crop.client = current_client;
        }
        out = {sizeof(out), texture, texture_width, texture_height,
            last_crop.client.left, last_crop.client.top, updated ? 1U : 0U, dropped,
            timestamp, std::max<int64_t>(0, qpc_100ns() - timestamp), serial};
        return 1;
    }

    void close() {
        require(GetCurrentThreadId() == thread_id, "Close capture on its creating thread");
        if (interop_device || interop_object || texture || locked) check_context();
        std::exception_ptr first_failure;
        const auto attempt = [&first_failure](auto&& action) {
            try { action(); return true; }
            catch (...) {
                if (!first_failure) first_failure = std::current_exception();
                return false;
            }
        };
        if (closed_subscribed) {
            attempt([&] { item.Closed(closed_token); });
            closed_subscribed = false;
        }
        if (session) { attempt([&] { session.Close(); }); session = nullptr; }
        if (pending_frame) { attempt([&] { pending_frame.Close(); }); pending_frame = nullptr; }
        if (pool) { attempt([&] { pool.Close(); }); pool = nullptr; }
        item = nullptr;
        // Always stop acquisition even if session close reports an error. GPU
        // resources, however, MUST remain alive if ownership release fails.
        // Never delete a locked or still-registered resource to force cleanup.
        if (attempt([&] { destroy_texture(); })) {
            if (interop_device && attempt([&] {
                check_win32(interop.close(interop_device), "wglDXCloseDeviceNV");
            })) interop_device = nullptr;
            if (!interop_device) {
                rt_device = nullptr;
                context = nullptr;
                device = nullptr;
                if (apartment_initialized) { RoUninitialize(); apartment_initialized = false; }
            }
        }
        if (first_failure) std::rethrow_exception(first_failure);
    }

    // Destruction is allowed only after explicit cleanup. Failed GPU ownership
    // release retains this entire object so its COM members cannot be destroyed.
    ~VhsCapture() = default;
};

uint32_t VHS_CALL vhs_capture_abi_version(void) { return 1; }

int32_t VHS_CALL vhs_capture_open(uintptr_t hwnd, VhsCapture** out_capture, char* error, uint32_t capacity) {
    return vhs_capture_open_ex(hwnd, VHS_CAPTURE_TRANSFER_GPU, out_capture, error, capacity);
}

int32_t VHS_CALL vhs_capture_open_ex(uintptr_t hwnd, uint32_t transfer, VhsCapture** out_capture, char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(out_capture != nullptr && *out_capture == nullptr, "Output capture handle must point to NULL");
        require(transfer == VHS_CAPTURE_TRANSFER_GPU || transfer == VHS_CAPTURE_TRANSFER_COMPATIBILITY,
            "Unknown capture transfer mode");
        auto capture = std::make_unique<VhsCapture>();
        try { capture->open(reinterpret_cast<HWND>(hwnd), transfer); }
        catch (...) {
            const auto open_failure = std::current_exception();
            try { capture->close(); }
            catch (...) {
                // The error boundary reports cleanup failure, and the host can
                // retry Close. Never unload the DLL with this live handle.
                *out_capture = capture.release();
                throw;
            }
            std::rethrow_exception(open_failure);
        }
        *out_capture = capture.release();
        return 1;
    });
}

int32_t VHS_CALL vhs_capture_get_stats(VhsCapture* capture, VhsCaptureStats* stats, char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(capture && stats && stats->struct_size == sizeof(VhsCaptureStats), "Invalid capture handle or statistics ABI size");
        capture->check_context();
        *stats = capture->stats;
        return 1;
    });
}

int32_t VHS_CALL vhs_capture_get_telemetry_v1(VhsCapture* capture, VhsCaptureTelemetryV1* telemetry,
    char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(capture && telemetry && telemetry->struct_size == sizeof(VhsCaptureTelemetryV1),
            "Invalid capture handle or telemetry v1 ABI size");
        capture->check_context();
        *telemetry = capture->telemetry;
        return 1;
    });
}

int32_t VHS_CALL vhs_capture_acquire(VhsCapture* capture, VhsCaptureFrame* frame, char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(capture && frame && frame->struct_size == sizeof(VhsCaptureFrame), "Invalid capture handle or frame ABI size");
        capture->check_context();
        capture->begin_telemetry();
        try {
            const auto result = capture->acquire(*frame);
            capture->end_telemetry();
            return result;
        } catch (...) {
            capture->end_telemetry();
            throw;
        }
    });
}

int32_t VHS_CALL vhs_capture_release(VhsCapture* capture, char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(capture != nullptr, "Capture handle is NULL");
        capture->check_context();
        capture->unlock();
        return 1;
    });
}

int32_t VHS_CALL vhs_capture_close(VhsCapture** capture, char* error, uint32_t capacity) {
    return boundary(error, capacity, [&]() -> int32_t {
        require(capture != nullptr, "Capture handle pointer is NULL");
        if (!*capture) return 1;
        (*capture)->close();
        delete *capture;
        *capture = nullptr;
        return 1;
    });
}
