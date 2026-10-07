# argus-backend

Коллектор Argus (Go-модуль `argus-collector`): следит за Docker-контейнерами и HTTP-таргетами одного хоста, ищет совпадения в логах, хранит всё в Postgres и шлёт алерты в Telegram. Наружу отдаёт HTTP API под `/v1/api`, его использует дашборд `../argus-frontend`.

## Что внутри

- **watcher** - события Docker (start/die/oom/kill/restart/destroy), статусы контейнеров, обнаружение crash-loop.
- **prober** - HTTP-проверки таргетов типа `probe`, переход в `down` после `prober.failure_threshold` неудач подряд.
- **matcher** - regex-правила по логам контейнеров.
- **alerter** - отправка алертов в Telegram с группировкой и cooldown.
- **host** - CPU, память и диск хоста (`GET /v1/api/host`), алерты по порогам.
- **retention** - чистка старых probes, events и log matches.
- **heartbeat** - dead man's switch: пока коллектор и база живы, раз в `heartbeat.interval` дёргает `HEARTBEAT_URL`.

Контейнеры попадают в мониторинг по лейблу `argus.monitor=true`. Если дополнительно задан `argus.probe.url`, для контейнера создаётся ещё и HTTP-таргет.

## Быстрый старт (Docker Compose)

```bash
cp .example.env .env              # заполнить POSTGRES_*, при желании BOT_TOKEN, CHAT_ID, HEARTBEAT_URL
cp config.example.yaml config.yaml
docker compose up -d --build
```

Коллектор слушает `127.0.0.1:8099`, Postgres доступен только во внутренней сети compose. Чтобы поднять вместе с дашбордом, запускайте `docker compose up -d --build` из корня репозитория: корневой `compose.yml` подключает этот файл.

Внутри контейнера `POSTGRES_URL` должен указывать на сервис `argus-postgres`, например `postgres://user:pass@argus-postgres:5432/argus?sslmode=disable`.

## Локальный запуск

Нужны Go 1.27+ и доступный Postgres.

```bash
cp .example.env .env              # POSTGRES_URL на локальный Postgres
cp config.example.yaml config.yaml
go run ./cmd/argus
```

Для watcher и matcher нужен доступ к Docker socket. Если Docker недоступен, они не стартуют, но остальной сервис работает.

## Конфигурация

Секреты и пути берутся из окружения (или из `.env`, он подхватывается автоматически):

| Переменная | Обязательна | Назначение |
|---|---|---|
| `CONFIG_PATH` | да | путь к `config.yaml` |
| `POSTGRES_URL` | да | строка подключения к Postgres |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` | для compose | учётные данные контейнера Postgres |
| `BOT_TOKEN`, `CHAT_ID` | нет | Telegram. Без них alerter не запускается |
| `HEARTBEAT_URL` | нет | URL для dead man's switch. Без него heartbeat не запускается |
| `HOST_DISK_PATH` | нет | переопределяет `host.disk_path` (в compose это `/hostfs`) |

Остальное настраивается в `config.yaml`, пример в `config.example.yaml`: адрес и таймауты HTTP-сервера, интервалы prober, crash-loop в watcher, retention, Telegram (`notify_interval`, `cooldown`), правила matcher, пороги host и `heartbeat.interval`. Длительности пишутся в формате Go (`336h`, `5m`), суффикса `d` нет. Severity в правилах matcher: `critical|error|warning|info|debug`.

При старте конфиг валидируется, с невалидным сервис не поднимется.

## Миграции

Миграции goose лежат в `migrations/`, вшиты в бинарник и применяются автоматически при старте (по транзакции на файл, под advisory lock Postgres). Отдельный CLI не нужен.

Новая миграция - следующий по номеру файл `migrations/0000N_name.sql` с секциями `-- +goose Up` / `-- +goose Down`. Если версия базы новее последней вшитой миграции, сервис откажется стартовать (защита от даунгрейда). После правки миграции или storage-запроса прогоните `make test-db`.

## API

- `GET /healthz` - healthcheck.
- `/v1/api` - targets (CRUD + `PATCH /{id}/status`), probes, events, log-matches (чтение и удаление), alerts (CRUD), host.
- Swagger UI: `http://127.0.0.1:8099/swagger/index.html`.

## Разработка

```bash
make test        # go test ./...
make test-db     # интеграционные тесты storage на временном postgres:16-alpine
make cover       # покрытие без сгенерированного кода
go vet ./...
golangci-lint run
```

- Моки: `mockery` после изменения интерфейсов в `internal/usecase/usecase.go`.
- Swagger: `swag init -g cmd/argus/main.go -d ./ -o ./docs` после изменения аннотаций хендлеров.
