Русский · English: README.en.txt

Изображения используются только в явно включённой браузерной демонстрации
архивной панели (?demo=1). Это реальные снимки буфера кадров GLSL через Mac CGL
с фиксированными сценой и интенсивностью 100%, а не живой захват Windows или
имитация произвольных значений ползунков. Предпросмотр архивной панели использует
рендерер Go. В текущем приложении только с треем встроенного предпросмотра нет.
CSS не создаёт эти изображения.

Повторное получение исходных изображений:
LUMATAPE_SHADER_OUTPUT=artifacts/macos-ux-validation/regression sh scripts/validate-shaders-macos.sh

polish-<preset>-<shape>.png соответствует здешнему <preset>-<shape>.png;
original.png получен из vhs-tape-native-original.png. CRT Classic и Soft TV
обновлялись с учётом масштаба Full-рендера. Subtle CRT и оба VHS-пресета/формы
побайтно совпадают с прежними изображениями. Локальные результаты и A/B-сравнения:
artifacts/macos-ux-validation/summary.json и demo-fixture-updates.json. Значения
параметров пресетов не менялись. Эти локальные артефакты не входят в публичный комплект.
