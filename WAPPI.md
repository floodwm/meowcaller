# meowcaller для gowapi

## Что подключается

Это Go-библиотека внутри процесса gowapi, а не отдельная служба. Она принимает
уже авторизованный `*go.mau.fi/whatsmeow.Client`. Новый клиент, QR и копирование
сессии не нужны. Whatsmeow закреплён на версии
`v0.0.0-20260929112325-8b41cfe6d9c4`, той же, что в gowapi.

`module github.com/purpshell/meowcaller` и прежние импорты сохраняются. Адрес
пользовательского форка задаётся в `replace` gowapi; переименовывать module и
все импорты для этого не требуется. Происхождение патчей: [PROVENANCE.md](PROVENANCE.md).

## Публикация и переход

1. Проверить diff и тесты в этом репозитории.
2. Закоммитить и отправить изменения в `origin` (floodwm/meowcaller).
3. Выбрать точный опубликованный SHA или release tag. Если используются теги,
   они должны быть уникальными и не перемещаться после публикации.
4. В gowapi заменить локальный `replace` на удалённый, закрепив версию форка.
   Схема записи (VERSION — Go semver/pseudo-version опубликованного коммита):

   ```go
   replace github.com/purpshell/meowcaller => github.com/floodwm/meowcaller VERSION
   ```

   Pseudo-version можно узнать через
   `go list -m -json github.com/floodwm/meowcaller@SHA`, затем взять поле `Version`.
   После замены выполнить `go mod tidy`, сборку и тесты gowapi. Только после
   успешной проверки удалить `gowapi/third_party/meowcaller` и изменить его
   документацию. Не использовать автоматически меняющийся HEAD в production.

До публикации форка основной go.mod gowapi продолжает ссылаться на существующий
third_party. Совместимость новой копии проверяется через временный `-modfile`,
без переключения запущенных профилей. Проверка опубликованной зависимости и
её доступности в сборочной среде выполняется после push.

## Получение обновлений оригинала

Удалённый `upstream` — `https://github.com/purpshell/meowcaller.git`.

```sh
git fetch upstream
git log --oneline HEAD..upstream/main
git diff HEAD...upstream/main
```

После просмотра обновлений объединить нужные изменения с нашей версией.
Переход upstream на hypermeow конфликтует с интеграцией gowapi: при разрешении
конфликтов сохранять официальный Whatsmeow, существующий клиент и наши тесты.
Затем выполнить проверки ниже, опубликовать новую версию форка и явно обновить
закреплённую версию в gowapi. Одно `git fetch` не обновляет зависимость gowapi.

## Проверки

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
```

Для интеграции gowapi: временный `-modfile` с replace на `/root/projects/meowcaller`,
`go test -vet=off -count=1 ./...`, целевая race-проверка Caller и сборка gowapi.
Параметр `-vet=off` сохранён из существующего порядка проверок gowapi;
библиотека проходит vet без исключения.

Примеры CLI/web и desktop malgo — отдельные вложенные Go-модули. Основной
VoIP-движок собирается без CGO; desktop-адаптер примеров использует CGO.

Это перенос уже существующих доработок. Новых возможностей, QR-авторизаций,
публичных gapi endpoints, перезапусков и живых звонков миграция не выполняет.
Проверки кода не доказывают live-совместимость с каждым клиентом WhatsApp.
