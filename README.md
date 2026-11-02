# Расписание без накладок

Go HTTP/REST + PostgreSQL. Требуется Docker и Docker Compose v2.

## Запуск одной командой

```bash
docker compose up --build
```

API: http://localhost:8080; health: `/health`; OpenAPI YAML: `/openapi.yaml`. Миграции применяются автоматически. Остановить `Ctrl+C`; удалить контейнеры и данные `docker compose down -v`.

## Быстрый пример

```bash
curl -s localhost:8080/users -H 'Content-Type: application/json' -d '{"name":"Анна"}'
curl -s localhost:8080/resources -H 'Content-Type: application/json' -d '{"name":"Переговорная 1","type":"room","capacity":6}'
curl -i localhost:8080/bookings -H 'Content-Type: application/json' -d '{"resource_id":1,"user_id":1,"start_at":"2030-01-10T10:00:00Z","end_at":"2030-01-10T11:00:00Z"}'
```

ID в ответах нужно использовать в следующих запросах. Время RFC3339 с часовым поясом.

## Маршруты

- `GET /resources?type=room&active=true`, `POST /resources`
- `GET /resources/{id}`, `PATCH /resources/{id}` (PATCH принимает все поля ресурса)
- `GET /resources/{id}/bookings?from=...&to=...`
- `GET /resources/{id}/availability?from=...&to=...&duration=30m`
- `GET /resources/available?from=...&to=...`
- `POST /users`, `GET /users/{id}/bookings?status=active&resource_id=1&from=...&to=...`
- `POST /bookings`, `GET /bookings/{id}`, `POST /bookings/{id}/cancel` с JSON `{"user_id":1}`
- `GET /health`, `GET /openapi.yaml`

Ошибки JSON-формата `{"error":{"code":"...","message":"..."}}`. Создание возвращает 201, конфликт расписания — 409, некорректный интервал — 422, чужая отмена — 403, исчерпание повторов сериализации — 503.

## Архитектура и гарантии

`cmd/server` — запуск и graceful shutdown; `internal/api` — HTTP и бизнес-сценарии; `internal/config` — конфигурация; `internal/store` — миграции; `migrations` — SQL; `docs/openapi.yaml` — спецификация OpenAPI 3.0.

PostgreSQL содержит `resources`, `users`, `bookings`, `schema_migrations`. Внешние ключи, `CHECK`-ограничения и индексы объявлены в миграции. Все интервалы полуоткрытые `[start_at,end_at)`: конфликт проверяется условиями `existing.start_at < requested.end_at AND existing.end_at > requested.start_at`, поэтому соседние интервалы разрешены.

Создание и отмена выполняются в транзакциях `SERIALIZABLE`. Проверка пересечений и вставка находятся в одной транзакции. При SQLSTATE `40001` вся транзакция повторяется с задержкой, число дополнительных попыток задаётся `SERIALIZABLE_RETRIES` (по умолчанию 3); бизнес-конфликт возвращает 409 без повторов. Локальные mutex не используются, поэтому несколько экземпляров приложения могут работать с общей БД.

Отмена сохраняет запись в истории, устанавливает `canceled_at`; повторная отмена идемпотентна. ID пользователя передаётся явно — это учебная схема, не полноценная аутентификация/авторизация. Для production необходимы проверка личности, TLS и секреты вместо dev-пароля.

## Настройки

`HTTP_ADDR` (`:8080`), `DATABASE_URL`, `MIN_BOOKING_DURATION` (`15m`), `MAX_BOOKING_DURATION` (`8h`), `SERIALIZABLE_RETRIES` (`3`), `SHUTDOWN_TIMEOUT` (`10s`), `MIGRATIONS_DIR` (`migrations`). JSON-логи содержат request ID, метод, путь и длительность. SIGINT/SIGTERM запускают graceful shutdown.

## Тесты

```bash
go test ./...
go test -race ./...
```

В архив включён unit-тест нормализации времени. Полноценные интеграционные конкурентные тесты на 100 запросов/несколько экземпляров в эту версию не включены; для строгой приёмки их необходимо добавить и запустить против настоящего PostgreSQL. Поэтому данный архив — запускаемый учебный проект, но не утверждение о полном покрытии всех тестовых критериев.
