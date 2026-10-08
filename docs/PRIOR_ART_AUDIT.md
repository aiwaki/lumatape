# Исходное архитектурное исследование

[Русский](PRIOR_ART_AUDIT.md) | [English](PRIOR_ART_AUDIT.en.md)

Это историческая запись исследования и сверки, выполненной 2026-10-03. Число
файлов и выводы ниже относятся к состоянию репозиториев на дату проверки.

По исходному заданию исследована [старая оконная библиотека пользователя](https://github.com/NeuralTeam/exwin/tree/efe4bef5c86bbc3017a2372a0fce77407c757cb7),
включая `examples/widget/widget.go`, оконный и рендерный слои, вспомогательные
функции Win32 и go.mod. На момент исследования AGENTS.md в том checkout отсутствовал.
Исходный репозиторий не изменялся. Ссылка сохраняет историю технических решений;
это не название и не зависимость LumaTape.

| Участок | Наблюдение | Решение |
|---|---|---|
| internal/window/init.go | GLFW.Init в init; Terminate вызывается из goroutine обработки сигналов | Явный запуск и остановка на начальном потоке |
| internal/window/window.go | Создание в goroutine, гонка window/err, reflect/busy-wait; ошибка запуска может оставить бесконечное ожидание | Синхронный конструктор с error; единый владелец ресурсов |
| internal/backend/backend.go | sync.Map смешивает callback рендера и команды; Load активно ждёт; порядок проходов не задан | Последовательный рендер и отдельная FIFO команд |
| internal/window/window_windows.go | Повторяющийся BringToTop, goroutine таймера; Stop не завершает range по ticker.C | Floating + WS_EX_NOACTIVATE; без циклов поднятия окна |
| pkg/window/affinity_windows.go | Результат SetWindowDisplayAffinity игнорируется | Проверка результата и чтение применённого значения, без гарантии для захвата монитора |
| examples/widget/widget.go | Canvas и robotgo используются ради текста и размера экрана | Прямой GLSL и физическая геометрия Win32 |
| go.mod | Старые версии GLFW/OpenGL, robotgo, Canvas, OCR и косвенные зависимости | Закреплённая GLFW 3.4; системные API; небольшая необязательная WGC DLL |

## Происхождение кода на дату сверки

Повторная сверка выполнена 2026-10-03. Прочитаны 16 Go-файлов исходной библиотеки,
включая пример. Они сравнены с 39 Go/C++/GLSL-файлами реализации LumaTape на тот
момент; совпадающих последовательностей из пяти или более непустых строк не
найдено. Дополнительно вручную сопоставлены жизненный цикл, бэкенд, GLFW hints,
Win32 styles и capture affinity. Проверка последовательностей — вспомогательное
свидетельство; вывод основан также на разборе функций.

Существенных перенесённых фрагментов, старых типов/очередей или исходного примера
в LumaTape не обнаружено. Жизненный цикл окон, загрузчик GLFW, рендер,
вспомогательные функции Win32 и мост захвата написаны заново. Общими остались
идея прозрачного окна и стандартные API/числовые флаги Windows/GLFW. Старая
библиотека не импортируется, не вендорится и не поставляется с приложением.

Прежняя корневая copyright-строка и отдельная копия лицензии исследованного
репозитория удалены из состава LumaTape: они ошибочно описывали происхождение
новой реализации. Корневой MIT LICENSE относится к коду LumaTape, Copyright (c)
2026 aiwaki. Лицензионные уведомления реально используемых GLFW, Go, C++/WinRT и
Microsoft STL сохранены в `third_party` и в правилах поставки.

Документация, проверенная при выборе API:

- https://www.glfw.org/docs/3.4/intro_guide.html#thread_safety
- https://www.glfw.org/docs/3.4/window_guide.html#window_transparency
- https://learn.microsoft.com/en-us/windows/apps/develop/media-authoring-processing/screen-capture
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwindowdisplayaffinity
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumdisplaysettingsw
- https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-changedisplaysettingsexw
- https://registry.khronos.org/OpenGL/extensions/NV/WGL_NV_DX_interop2.txt
