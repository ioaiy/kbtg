# Backend-test-golang

Два эндпоинта: список предметов Skinport.

## Stack

`Go 1.22` · `chi` · `pgx/v5` · `go-redis/v9` · `shopspring/decimal` · `andybalholm/brotli` · `golang.org/x/sync` · `slog` · `golang-migrate` · `swaggo` · `testify` · `testcontainers-go`

## Quick start

```bash
cp .env.example .env
make tidy-docker        # сгенерирует go.sum
make docker-build       # собрать образы
make docker-up          # поднять стек в фоне
```

Проверка после старта (smoke test):

```bash
# 1. Health / readiness
curl -s http://localhost:8080/healthz | jq
curl -s http://localhost:8080/readyz  | jq

# 2. Skinport items — первый запрос идёт в upstream, кешируется в Redis
curl -s 'http://localhost:8080/v1/items?app_id=730&currency=USD' | jq '.[0:2]'

# Повторный запрос в пределах TTL=60s — приходит из кеша (X-Cache: hit).
curl -s -D - -o /dev/null \
     'http://localhost:8080/v1/items?app_id=730&currency=USD' | grep -i x-cache

# 3. Текущий баланс — на старте 100.00
curl -s http://localhost:8080/v1/users/1/balance | jq
# → { "user_id": 1, "balance": "100.00" }

# 4. Debit — попытка списать больше, чем есть (баланс 100.00, просим 101.00).
#    История не пишется, баланс не меняется.
curl -s -X POST http://localhost:8080/v1/users/1/debit \
     -H 'Content-Type: application/json' -d '{"amount":"101.00"}' | jq
# → 400 INSUFFICIENT_BALANCE

# 5. Debit — невалидная сумма: NUMERIC(20, 2) хранит максимум 2 знака
#    после точки. Сервер не округляет сам — это решение клиента.
curl -s -X POST http://localhost:8080/v1/users/1/debit \
     -H 'Content-Type: application/json' -d '{"amount":"50.123"}' | jq
# → 400 INVALID_AMOUNT

# 6. Несуществующий пользователь — оба эндпоинта дают 404
curl -s -X POST http://localhost:8080/v1/users/999/debit \
     -H 'Content-Type: application/json' -d '{"amount":"10.00"}' | jq
curl -s http://localhost:8080/v1/users/999/balance | jq
# → 404 USER_NOT_FOUND

# 7. Debit — happy path. Списываем ровно 100.00, баланс уйдёт в 0.00
curl -s -X POST http://localhost:8080/v1/users/1/debit \
     -H 'Content-Type: application/json' -d '{"amount":"100.00"}' | jq
# → 200 { balance_before: "100.00", amount: "100.00", balance_after: "0.00" }

# Подтверждение через GET balance
curl -s http://localhost:8080/v1/users/1/balance | jq
# → { "user_id": 1, "balance": "0.00" }

# 8. Edge: даже копейка теперь не пройдёт
curl -s -X POST http://localhost:8080/v1/users/1/debit \
     -H 'Content-Type: application/json' -d '{"amount":"0.01"}' | jq
# → 400 INSUFFICIENT_BALANCE

# 9. Swagger UI
open http://localhost:8080/v1/swagger/index.html
```

## API

| Method | Path                          | Описание                                                          |
|--------|-------------------------------|-------------------------------------------------------------------|
| GET    | `/v1/items`                   | Skinport items с кешем (TTL=60s, stale-on-error)                  |
| GET    | `/v1/users/{id}/balance`      | Текущий баланс пользователя                                       |
| POST   | `/v1/users/{id}/debit`        | Атомарное списание с записью в `balance_history`                  |
| GET    | `/healthz` · `/readyz`        | Liveness / readiness                                              |
| GET    | `/v1/swagger/index.html`      | OpenAPI UI (только при `APP_ENV != production`)                   |

Формат ошибок единый: `{ "error": "...", "code": "...", "request_id": "..." }`.

Коды: `INVALID_REQUEST` · `INVALID_AMOUNT` · `INVALID_USER_ID` · `USER_NOT_FOUND` · `INSUFFICIENT_BALANCE` · `UPSTREAM_ERROR` · `INTERNAL_ERROR`.

## Project layout

```text
cmd/server          entrypoint, DI, graceful shutdown
internal/config     ENV → typed Config
internal/httpapi    Server + handlers (Mat Ryer NewServer pattern)
internal/balance    debit service + Postgres repo (SELECT FOR UPDATE)
internal/skinport   upstream client + service (singleflight, stale-on-error)
internal/cache      RedisCache, MemoryCache (interface объявлен у consumer'а)
internal/platform   postgres pool, redis client, slog setup
migrations          golang-migrate SQL
docs                сгенерированная OpenAPI-спека (swag init) + placeholder
deployments         Dockerfile, docker-compose.yml
```

## Make

```text
make run               запустить локально
make build             собрать бинарь
make test              unit (локально или в Docker, авто-детект)
make test-integration  + testcontainers (локально или в Docker)
make clean-test-cache  очистить Docker-кеш Go-модулей (форс пересборка)
make tidy              go mod tidy локально
make tidy-docker       go mod tidy внутри Docker (если Go не установлен)
make swag              сгенерировать OpenAPI локально (требует swag CLI)
make swag-docker       сгенерировать OpenAPI внутри Docker
make migrate-up        накатить миграции
make docker-build      собрать образы compose
make docker-up         поднять стек в фоне (detached)
make docker-up-fg      поднять стек с логами в терминале (Ctrl+C для остановки)
make docker-logs       тейлить логи (опц. s=app — конкретный сервис)
make docker-stop       пауза (контейнеры остаются, БД сохраняется)
make docker-down       полный teardown: контейнеры + volumes (БД обнулится)
```

## Tests

`make test` и `make test-integration`. Сбросить кеш: `make clean-test-cache`.

Главный тест корректности — `TestPgRepo_RaceCondition`: 50 параллельных дебитов на 10 долларов при балансе 100 долларов, ожидается ровно 10 успешных. Подтверждает работу `SELECT FOR UPDATE`.

## Production gaps (что сознательно не реализовано)

### Идемпотентность

Двойной POST на `/debit` приведёт к двойному списанию. Production:
- `Idempotency-Key` от клиента.
- Таблица `idempotency_keys (key, user_id, response_hash, created_at)` с уникальным индексом `(user_id, key)`.
- Middleware: при повторе ключа возвращать сохранённый ответ; при коллизии (тот же ключ, другое тело) — `422`.
- TTL-очистка через `pg_cron`.

### Аутентификация и авторизация

Любой клиент может списать с любого `user_id`. Production: JWT/session с проверкой совпадения identity и `user_id` в URL.

### Rate limiting

Без auth не имеет смысла (по IP обходится). После добавления auth — token bucket per-user через `golang.org/x/time/rate` + Redis для распределённого случая.

### Метрики и трейсинг

Не подключены: без Prometheus/Jaeger в compose это код, который никуда не пишет. Метрики, которые имеет смысл собирать:
- `http_request_duration_seconds{method,path,status}`
- `debit_requests_total{outcome="success|insufficient|error"}`
- `skinport_cache_hit_ratio`
- `db_tx_duration_seconds`
- OTel spans вокруг `Skinport call`, `DB tx`, `Cache op`.

### Circuit Breaker для Skinport upstream

Сейчас при недоступном Skinport каждый cold-cache запрос ждёт полный таймаут (`SKINPORT_TIMEOUT_SECONDS`) перед fallback'ом на stale. Это съедает HTTP-воркеры и ухудшает latency для всех остальных запросов под нагрузкой. Production-вариант — `sony/gobreaker` с триггером на N consecutive failures, сразу возвращающий запросы в stale при `open` state. Не реализовано: для текущего нагрузочного профиля (1 инстанс, low QPS) выигрыш маржинальный, а stale-on-error уже даёт корректный fallback.

К `/debit` Circuit Breaker сознательно **не применяем** — Postgres у нас primary store без fallback'а, fail-fast на write-операции вреднее, чем подождать. Защита там — `lock_timeout` + `statement_timeout` + bounded pool, плюс idempotency на клиенте.

### Refund / пополнение

Добавляется симметричным `POST /v1/users/{id}/credit` с `operation_type` enum в `balance_history`.

### Deep readiness check

`/readyz` возвращает `200` всегда. Production: `pool.Ping` + `redisClient.Ping` с коротким таймаутом, `503` при недоступности зависимостей.
