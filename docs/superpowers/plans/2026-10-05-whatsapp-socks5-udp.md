# WhatsApp media over profile SOCKS5 — Implementation Plan

**Goal:** Передавать WhatsApp media через SOCKS5 профиля gowapi без прямого fallback.
**Architecture:** Fork предоставляет CallWithMediaProxy(ctx, target, proxyURL), сохраняющий proxy URL на сессию. Relay получает net.PacketConn с RFC1928 UDP framing вместо прямого socket. Gowapi использует актуальную main/reserve proxy профиля при start. Без proxy библиотека сохраняет прямой режим; с HTTP proxy возвращает явную ошибку.
**Tech Stack:** Go, net/context, RFC1928/1929, существующий Pion DTLS/SCTP.
**Constraints:** Авторизация Whatsmeow сохраняется. Не менять production, не коммитить/не пушить. URL с credentials и signaling/media payload не логировать. Нет direct fallback. Existing module pin остаётся опубликованным; локальное новое API проверяется через временный Go workspace. Старый fork распознаётся через интерфейс CallWithMediaProxy и с настроенной proxy отказывает до вызова, без обхода.

## 1. Fork transport
- [x] Локальные tests SOCKS server: RFC framing IPv4/IPv6, authenticated UDP roundtrip, unsupported/auth errors, TCP closure, context cancellation, deadline, malformed/fragmented frames, no direct fallback.
- [x] Добавить relay/socks5_udp.go: ValidateSOCKS5Proxy, DialSOCKS5UDP(ctx,url), connected proxy UDP socket, живой TCP control, PacketConn lifecycle, bounded buffers, sanitized stage errors.
- [x] Relay options WithSOCKS5Proxy; ConnectRelayMediaContext сохраняет Context cancellation; существующий ConnectRelayMedia — wrapper.
- [x] Client.CallWithMediaProxy: snapshot в engineCall, проверка URL до offer, прокидывание transport option. Timeout отменяет зависший setup и закрывает late results.

## 2. Gowapi
- [x] Tests: выбор main/reserve profile proxy, новый engine получает proxy, старый engine не вызывает прямой Call при наличии proxy, nil/config errors, публичная ошибка.
- [x] Добавить caller_wa_proxy.go с интерфейсом версии engine. Init place wrapper берёт атомарный snapshot текущей main/reserve proxy при start; все переключения пула обновляют snapshot. Отдельные media_proxy_failed/media_proxy_unsupported причины и UI текст.
- [x] Проверить paired gapi на dev2; API command unchanged.

## 3. Verification and delivery
- [x] Fork focused tests + race + полный suite/build.
- [x] Gowapi published pin build + tests, workspace fork build/tests + race.
- [x] Обновить CHANGELOG, datasheet и инструкции публикации fork/pin без локального replace в go.mod.
- [x] Живую работу отмечать отдельно от локального UDP теста: две реальные proxy ранее не дали UDP responses. Не обещать, что новая реализация исправляет сетевой доступ.

## Delivery state

Исходники подготовлены без commit/push. Проверка опубликованного pin подтверждает совместимость сборки и безопасный отказ старого движка; проверка новой реализации выполняется через `/tmp/caller-wa-socks-workspace/go.work`. Новый dev-бинарник: `/tmp/gowapi-wa-socks-udp`. Fork сначала нужно опубликовать, затем обновить git revision в go.mod gowapi; normal release dependency пока не обновлена. Работающие сервисы и production для этой задачи не изменены.

Проверка новым DialSOCKS5UDP через proxy текущего профиля 783160ee-a291: UDP ASSOCIATE успешен; ответов DNS от 1.1.1.1:53 и 8.8.8.8:53 за 2 секунды каждый нет. Это не live WhatsApp validation и не доказательство блокировки конкретных media relay.
