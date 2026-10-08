# Исходное архитектурное исследование

По исходному заданию исследована [старая оконная библиотека пользователя](https://github.com/NeuralTeam/exwin/tree/efe4bef5c86bbc3017a2372a0fce77407c757cb7), включая `examples/widget/widget.go`, оконный/рендерный слой, Win32 helpers и go.mod. AGENTS.md в этом checkout отсутствует. Исходный репозиторий не изменялся. Ссылка сохраняется здесь как история технических решений, а не название или зависимость LumaTape.

| Участок | Наблюдение | Решение |
|---|---|---|
| internal/window/init.go | GLFW.Init в init; Terminate вызывается из signal goroutine | Явный запуск/остановка на начальном потоке |
| internal/window/window.go | Создание в goroutine, гонка window/err, reflect/busy-wait, startup error может навечно оставить ожидание | Синхронный конструктор с error; одно владение ресурсами |
| internal/backend/backend.go | sync.Map смешивает render callback и commands; Load активно ждёт; порядок проходов не задан | Последовательный рендер и отдельная FIFO команд |
| internal/window/window_windows.go | Повторяющийся BringToTop, timer goroutine; Stop не завершает range по ticker.C | Floating + WS_EX_NOACTIVATE; никаких циклов поднятия |
| pkg/window/affinity_windows.go | Результат SetWindowDisplayAffinity игнорируется | Проверка результата и readback, без заявления о гарантии для monitor capture |
| examples/widget/widget.go | Canvas и robotgo используются ради текста и размера экрана | Прямой GLSL и физическая геометрия Win32 |
| go.mod | Старые версии GLFW/OpenGL, robotgo, Canvas, OCR и косвенные зависимости | GLFW 3.4 pinned; системные API; маленькая optional WGC DLL |

## Происхождение текущего кода

Повторная сверка выполнена 2026-10-03. Прочитаны 16 Go-файлов исходной библиотеки, включая пример. Они сравнены с 39 Go/C++/GLSL-файлами текущей реализации; совпадающих последовательностей из пяти или более непустых строк не найдено. Дополнительно вручную сопоставлены lifecycle, backend, GLFW hints, Win32 styles и capture affinity. Проверка последовательностей — вспомогательное свидетельство; вывод основан также на разборе функций.

Существенных перенесённых фрагментов, старых типов/очередей или исходного примера в LumaTape не обнаружено. Оконный lifecycle, загрузчик GLFW, рендер, Win32 helpers и capture bridge написаны заново. Общими остались идея прозрачного окна и стандартные API/числовые flags Windows/GLFW. Старая библиотека не импортируется, не вендорится и не поставляется с приложением.

Прежняя корневая copyright-строка и отдельная копия лицензии исследованного репозитория удалены из состава LumaTape: они ошибочно описывали происхождение новой реализации. Корневой MIT LICENSE относится к коду LumaTape, Copyright (c) 2026 aiwaki. Notices реально используемых GLFW, Go, C++/WinRT и Microsoft STL сохранены в `third_party` и в правилах поставки.

Документация, проверенная при выборе API:

- https://www.glfw.org/docs/3.4/intro_guide.html#thread_safety
- https://www.glfw.org/docs/3.4/window_guide.html#window_transparency
- https://learn.microsoft.com/en-us/windows/apps/develop/media-authoring-processing/screen-capture
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowdisplayaffinity
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumdisplaysettingsw
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-changedisplaysettingsexw
- https://registry.khronos.org/OpenGL/extensions/NV/WGL_NV_DX_interop2.txt
