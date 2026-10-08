# Вариативный шрифт Mona Sans

**Русский** · [English](README.en.md)

Официальный шрифт GitHub хранится локально без изменений. Во время работы CDN
и сетевые запросы за шрифтом не нужны. Сокращено только имя локального файла
до `mona-sans.woff2`; внутренние имена и таблицы не менялись. Это официальное
семейство, но побайтное совпадение со шрифтом githubuniverse.com не утверждается.

- Репозиторий: https://github.com/github/mona-sans
- Тег релиза: `v2.0.27`
- Закреплённый коммит: `0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca`
- Исходный путь: `fonts/webfonts/variable/MonaSansVF[wdth,wght,opsz,ital].woff2`
- [Закреплённая загрузка](https://raw.githubusercontent.com/github/mona-sans/0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca/fonts/webfonts/variable/MonaSansVF%5Bwdth,wght,opsz,ital%5D.woff2)
- Дата загрузки: 2026-10-07
- Размер: **532 968 байт**
- SHA256: `fd40288d051171b51e3d01f36790604470dbb4d4fc5b36ee5a8119f4f4c6b3e1`
- Git blob SHA1, независимо сверенный с закреплённым деревом репозитория:
  `f12b5e3d6853567a21a721b229a7ae9160f674a7`

Исходные `LICENSE` и `OFL.txt` сохранены в `MonaSans-LICENSE.txt` и `MonaSans-OFL.txt`.
Оба содержат полный текст SIL Open Font License 1.1 и соответствующие уведомления
об авторских правах. Добавляйте их в уведомления распространяемого бинарного пакета:
сам импорт WOFF2 через CSS не заставляет Vite копировать лицензии. Внутренний
copyright шрифта соответствует `OFL.txt` (2022 Mona Sans Project Authors,
Reserved Font Name "Mona").

## Фактические таблицы шрифта

Таблицы проверены напрямую через FontTools 4.66.1 и Brotli 1.2.0. Шрифт
не конвертировали, символы не удаляли, внутренние имена не переименовывали
и других изменений не вносили.

- Family / typographic family: `Mona Sans VF`
- Subfamily: `Regular`
- PostScript name: `MonaSansVF-Regular`
- Version string: `Version 2.027;Glyphs 3.4.1 (3436)`

| Ось | Минимум | Значение по умолчанию в файле | Максимум |
| --- | ---: | ---: | ---: |
| `wdth` (ширина) | 75 | 100 | 125 |
| `wght` (насыщенность) | 200 | 200 | 900 |
| `opsz` (оптический размер) | 0 | 0 | 100 |
| `ital` (курсив) | 0 | 0 | 1 |

Это значения метаданных этого файла, а не рекомендуемый CSS основного текста.
Задавайте насыщенность явно, например 400. Локальный `@font-face` может использовать
CSS-псевдоним `Mona Sans`, `font-weight: 200 900` и `font-stretch: 75% 125%`;
внутреннее семейство остаётся `Mona Sans VF`.

## Ограничение кириллицы

Лучшая Unicode cmap содержит 568 кодовых точек, включая все 95 печатных ASCII-символов.
В ней **нет символов U+0400–U+052F** и **0/66 русских прописных/строчных букв,
включая Ё/ё**. Нельзя утверждать, что русский интерфейс отображается Mona Sans.

В стеке `"Mona Sans", "Segoe UI Variable", "Segoe UI", system-ui, sans-serif`
браузер использует Mona Sans для поддержанных символов и следующий локальный
шрифт для кириллицы. В Windows это обычно Segoe UI Variable или Segoe UI,
в зависимости от установки; `system-ui` покрывает другие ОС. Смешанные строки
латиницы и кириллицы могут заметно отличаться формой букв и метриками.
Дополнительный fallback-шрифт не загружался и не включался в комплект.

## Повторная загрузка

Из корня репозитория:

```sh
curl --fail --location \
  'https://raw.githubusercontent.com/github/mona-sans/0f7dc66ddd766605eb0e75c3f47bf9d1dd38ceca/fonts/webfonts/variable/MonaSansVF%5Bwdth,wght,opsz,ital%5D.woff2' \
  --output desktop/ui/assets/fonts/mona-sans.woff2
(cd desktop/ui/assets/fonts && shasum -a 256 -c SHA256SUMS)
```

Шрифт относится к архивной панели; текущий нативный трей использует отрисовку
меню ОС. Точные метаданные ресурса также записаны в `provenance.json`.
