# Инструменты проверки Windows UI

Русский · [English](README.en.md)

Инструменты разработчика в отдельном Go-модуле. Сам LumaTape не зависит от makc и не внедряет события ввода. Собирать можно на любой системе с Go; запускать — только в явно выбранном тестовом сеансе Windows:

```sh
cd scripts/windows-ui-smoke
go test -race ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-ui-smoke.exe .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-source-input.exe ./cmd/source-input
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../artifacts/production-polish/lumatape-pointer-input.exe ./cmd/pointer-input
```

Инструменты требуют точные PID и путь EXE. Они удерживают дескриптор процесса, проверяют путь живого процесса, владельца и класс HWND и никогда не завершают присоединённые процессы принудительно. Команда `quit` для Settings явно запрашивает штатное закрытие проверенного контроллера с тем же PID. Среда, запускающая приложение и тестовую сцену, должна сохранять и завершать только собственные процессы по удерживаемым дескрипторам/PID. Эти инструменты не запускают и не ищут процессы по имени файла для завершения.

## Элементы нативного окна настроек

Сначала откройте настройки, затем выполните осмотр или шаги из UTF-8 JSON-массива:

```powershell
.\lumatape-ui-smoke.exe -pid $app.Id -exe $app.Path -output inspect.jsonl
.\lumatape-ui-smoke.exe -pid $app.Id -exe $app.Path -steps steps.json -config test-config.json -log lumatape.log -output result.jsonl
```

Запись в файл исключает преобразование кодировки в конвейере внешних команд PowerShell 5. Каждый шаг записывается отдельной UTF-8 JSON-строкой с фактическими элементами интерфейса и, при необходимости, конфигурацией с диска и концом журнала. Несовпадение ожидаемого результата завершает инструмент с кодом 1. Класс окна должен быть `LumaTape.Settings`. Сообщения `WM_SETTEXT`, `CB_SETCURSEL` с `CBN_SELCHANGE` и `BM_CLICK` отправляются с тайм-аутом две секунды. Это проверяет обработку элементов приложения, но не доставку событий физической клавиатуры или мыши.

```json
[
  {"op":"select","id":3007,"index":4},
  {"op":"wait","ms":200},
  {"op":"expect-select","id":3007,"index":4},
  {"op":"text","id":3008,"text":"50"},
  {"op":"click","id":3019},
  {"op":"wait","ms":700},
  {"op":"expect-config","field":"effects.intensity","value":0.5},
  {"op":"expect-text","id":3029,"text":"Настройки применены"}
]
```

Действия: `inspect`, `foreground`, `select` (ID/index), `text` (ID/text), `check` (ID/index 0 или 1), `click` (ID), `wait` (0..5000 мс), `quit` (только последний шаг). `quit` отправляет `WM_CLOSE` окну `LumaTape.Control` того же PID, ждёт фактического выхода до пяти секунд, требует код 0 и сохраняет конфигурацию и журнал после завершения. Команда работает и без открытого окна настроек.

Проверки: `expect-select`, `expect-text` (подстрока), `expect-check`, `expect-visible`, `expect-enabled` (index 0 или 1), `expect-config` (поле с точками/точное JSON-значение), `expect-log` (подстрока в последних 32 KiB). Перед изменениями проверьте текущие ID и варианты: порядок источников может меняться. `expect-log` может совпасть со старым событием; для подтверждения нового перехода используйте свежий журнал или сравните сеанс и время в собранных записях.

Инструмент не подтверждает и не выбирает системный видеорежим автоматически. Для изменения видеорежима нужен явный тестовый сценарий со штатным запросом подтверждения и процессом восстановления watchdog.

## Реальный ввод в тестовую сцену через makc

`makc` закреплён на v0.2.0, коммит `31d0078d4ad8f3c10423016974a698280c2939f2`. Здесь он вызывает Win32 `SendInput` для проверки клика сквозь эффект, перемещения и изменения размера окна, доставки горячих клавиш в Windows. Исходные лицензии makc и x/sys сохранены в `licenses/`; лицензия среды выполнения Go — в `third_party/Go-LICENSE.txt` корня репозитория.

```powershell
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action inspect -output source-before.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action click -output click.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action drag -dx 60 -dy 40 -output drag.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action resize -dx 80 -dy 30 -output resize.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action keys -keys ctrl+shift+9 -output keys.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action minimize -output minimized.json
.\lumatape-source-input.exe -pid $card.Id -exe $card.Path -action restore -output restored.json
```

Класс источника должен быть `LumaTape.Native.TestCard`. Инструмент использует per-monitor DPI v2 и физические координаты виртуального рабочего стола. Он явно запрашивает фокус, проверяет владельца и активное окно до ввода и на каждом шаге перетаскивания, отклоняет уже зажатые кнопки и клавиши и отпускает собственные нажатия при ошибке. `minimize`/`restore` вызывают `ShowWindow` для проверенного источника и проверяют фактический `IsIconic`; отчёт включает прямоугольники до и после действия. Перемещение и изменение размера должны менять прямоугольник окна; клик — увеличивать свойство `LumaTape.TestCard.Clicks` ровно на один. Свойство хранит видимый на сцене счётчик плюс один. Старые сцены без свойства не проходят проверку клика.

Успешный ввод сочетания сам по себе не подтверждает доставку команды. Затем проверьте окно настроек через `expect-text`/`expect-config`, счётчики команд в диагностике и видимый эффект. Windows `SendInput` не проверяет путь физической Mac-клавиатуры Option/Fn → Parallels. Скрипты не меняют горячие клавиши хоста, настройки VM, системную скорость мыши и не устанавливают перехватчики ввода.

Контракты Microsoft API: [SendMessageTimeoutW](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-sendmessagetimeoutw), [CB_SETCURSEL](https://learn.microsoft.com/en-us/windows/win32/controls/cb-setcursel), [BM_CLICK](https://learn.microsoft.com/en-us/windows/win32/controls/bm-click).

## Проецируемый курсор

Соберите отдельную сцену из корня репозитория командой `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o artifacts/production-polish/lumatape-testcard.exe ./cmd/lumatape-testcard`, затем запустите её в Windows с `-pointer-test`. Заголовок — `LumaTape pointer test`; обычная тестовая сцена не меняется. Выберите именно это окно в LumaTape и включите статическую выпуклость перед проверкой. Независимая проверка координат предполагает одинаковые физические границы источника и оверлея, отсутствие обрезки, изменения DAR и эффектов jitter/tracking. Кривизна шейдера должна совпадать со значением `-curvature`.

```powershell
.\lumatape-testcard.exe -pointer-test -width 960 -height 720
.\lumatape-pointer-input.exe -pid $sourcePID -exe $sourceExe -worker-pid $workerPID -worker-exe $workerExe -action targets -curvature 0.7 -output targets.json
.\lumatape-pointer-input.exe -pid $sourcePID -exe $sourceExe -worker-pid $workerPID -worker-exe $workerExe -action drag -target 5 -dx 80 -dy 40 -curvature 0.7 -output drag.json
```

Действия: `inspect`, `move`, `targets` (наведение/клик по всем пяти целям), `drag`, `wheel` (+120) и `held-disable` (зажать левую кнопку, отправить `ctrl+shift+0`, проверить восстановление и отпустить кнопку). `-target` выбирает цель 1–5; `-disable-keys` меняет явное сочетание. По умолчанию используется `-projection visible`; `hidden` требует восстановленного системного курсора, а `ignore` служит только исходной проверкой без проверки проекции. PID и путь EXE рабочего процесса обязательны, кроме режима `ignore`.

Инструмент проверяет нативные события источника, реальные координаты курсора, владельца захвата мыши, идентичность рабочего процесса и фактическую позицию окна курсора с учётом нативной активной точки. Наведение ожидается как результат события независимо от достижения курсором позиции. JSON пишется напрямую в UTF-8; код выхода 1 означает провал проверки.

## Жизненный цикл и срок действия кадра

Два ручных протокола находятся на каталог выше. Оба скрипта сохраняют UTF-8 BOM для Windows PowerShell 5, требуют существующий каталог вывода и записывают JSON при ошибке:

```powershell
..\windows-pointer-lifecycle-smoke.ps1 -RuntimeJson candidate-runtime.json -HelperPath .\lumatape-pointer-input.exe -Output lifecycle.json -Curvature 0.7
```

Метаданные жизненного цикла должны содержать `Bundle` (абсолютный каталог проверяемого комплекта), `HostPID` и `SourcePID`. Скрипт разрешает только цепочку host → engine → pointer-worker этого комплекта, фиксирует нативное время создания процессов и идентичность HWND и проверяет minimize → restore → resize → закрытие источника. После restore/resize инструмент ввода проверяет соответствие координат новым границам. **Закрытие именно этой pointer-test сцены — последнее действие.** Повторного запуска нет. Ошибка до закрытия восстанавливает исходный внешний прямоугольник даже при отказе engine/worker. После собственного `WM_CLOSE` выход подтверждается удерживаемым дескриптором; метаданные образа завершающегося процесса не читаются.

```powershell
..\windows-pointer-lease-smoke.ps1 `
  -EngineProcessId $enginePID -EngineExecutable $engineExe -EngineCreatedFileTime $engineCreated `
  -WorkerProcessId $workerPID -WorkerExecutable $workerExe -WorkerCreatedFileTime $workerCreated `
  -SourceProcessId $sourcePID -SourceExecutable $sourceExe -SourceCreatedFileTime $sourceCreated `
  -Output lease.json
```

Для проверки срока действия кадра нужны точные PID, абсолютные пути EXE и десятичные FILETIME создания всех трёх процессов из `GetProcessTimes`; берите их из диагностических проверок, а не округлённых временных меток CIM. Начальное состояние — активная проекция над активным источником, без зажатых кнопок мыши. Кратковременно приостанавливается только движок. Возобновление защищено watchdog на 1500 мс и блоками C#/PowerShell `finally`; измеренная пауза должна укладываться в 2000 мс. Проверяются возврат системного курсора после истечения 500-мс срока действия кадра и восстановление по свежему кадру. `OverlayHiddenDuringSuspension` — отдельная проверка: `ShowWindowAsync` может ждать приостановленный движок, поэтому возврат курсора сам по себе не доказывает исчезновение устаревшего оверлея.

## Длительная проверка захвата

Для длительной проверки захвата `pointer-input -action motion -duration 90s -projection ignore` двигает курсор по детерминированному пути внутри проверенной сцены. Длительность ограничена 1–180 секундами; фокус, геометрия и кнопки контролируются весь прогон. Каждую секунду проверяются реальный курсор и доставленные нативные координаты мыши. Кнопки не нажимаются. Параллельно запускайте независимое наблюдение за видимостью оверлея: это действие не проверяет положение курсора при crop/warp. Для запуска без консоли нужны `ProcessStartInfo.CreateNoWindow=true` и `UseShellExecute=false`, **не** `Start-Process -WindowStyle Hidden`: последний передаёт `STARTF_USESHOWWINDOW/SW_HIDE`, переопределяет первый `ShowWindow` инструмента и может скрыть саму тестовую сцену.

`windows-overlay-visibility-smoke.ps1 -Runtime candidate-runtime.json -OutputDirectory <existing directory> -Label moving -Seconds 95` независимо наблюдает за привязанным к идентичности оверлеем примерно каждые 10 мс; планировщик Windows может увеличить интервал. Запись о запущенных процессах должна содержать точные `Bundle`, `HostPID` и `SourcePID`. Отчёт содержит фокус источника, интервалы невидимости, координаты курсора и прошедшее время, а также исходные CSV-выборки. Ввод и скриншоты не выполняются. Нулевое число скрытий значимо только при действительно включённом нужном эффекте/формате и сохранённом фокусе источника: проверьте меню/конфигурацию и сопоставьте тот же UTC-интервал с журналом движка. Измерения производительности сборки оболочки и другие действия в интерфейсе выполняйте отдельно.

Эти протоколы проверяют гостевые Win32-события и жизненный цикл принадлежащих тесту окон. Они не проверяют физический ввод Mac/Parallels, произвольные игры, аппаратный GPU-путь или видимость в композиторе только по `CURSOR_SHOWING`. Инструменты разработчика не добавляют в продукт переназначение или внедрение игрового ввода.
