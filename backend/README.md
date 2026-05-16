# Backend

Backend SVAP Query Service - Go-приложение с REST API для сохраненных запросов, выполнения запросов к СВАП, хранения результатов, справочников и динамической конфигурации.

## Архитектура

Код разложен по слоям в духе Clean Architecture:

- `cmd/server` - сборка приложения, wiring зависимостей, HTTP server, запуск миграций.
- `internal/api` - HTTP handlers, валидация request/response, Swagger annotations.
- `internal/service` - use cases: сохраненные запросы, выполнение, результаты, справочники.
- `internal/ports` - интерфейсы репозиториев и внешних зависимостей.
- `internal/repository` - PostgreSQL adapters и migration runner.
- `internal/svap` - HTTP-клиент СВАП.
- `internal/config` - динамическая конфигурация и TTL результатов.
- `internal/model` - доменные модели.
- `docs` - сгенерированная Swagger/OpenAPI документация.
- `db/migrations` - SQL-миграции и seed-данные.

Сервисный слой зависит от портов, а не от PostgreSQL или конкретного SVAP transport adapter.

## База данных

Основные таблицы:

- `analytical_attributes` - аналитические признаки.
- `dictionaries_cache` - конфигурационные и бизнес-справочники.
- `saved_queries` - сохраненные запросы пользователей.
- `query_executions` - задачи выполнения запросов.
- `query_results` - метаданные результатов.
- `query_result_rows` - строки результата.
- `query_row_values` - значения строк результата.
- `app_config` - динамическая конфигурация.
- `schema_migrations` - история выполненных миграций.

Миграции:

- `db/migrations/000001_init_schema.sql` - схема и базовые данные аналитических признаков.
- `db/migrations/000002_config_data.sql` - справочники настройки интерфейса: типы запросов, допустимые операции, колонки, индикаторы, result-поля.
- `db/migrations/000003_load_data.sql` - загрузка бизнес-справочников из `db/migrations/data/business_dictionary_items.tsv`.

Миграции выполняются автоматически при старте. Целевая схема берется из `DB_SCHEMA`, по умолчанию `svap_query_service`.

Для нескольких экземпляров приложения используется PostgreSQL advisory lock. Все операции с `schema_migrations` идут через соединение, которое держит lock, поэтому параллельный старт backend'ов с одной БД безопасен.

## Конфигурация

Переменные окружения:

```bash
DB_HOST=postgres
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=svap_query_service
DB_SCHEMA=svap_query_service
DB_SSLMODE=disable
SERVER_PORT=8080
SVAP_TIMEOUT_SECONDS=180
SVAP_INSECURE_SKIP_VERIFY=false
```

Адреса СВАП по умолчанию:

```bash
SVAP_FSG_HOST=http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081
SVAP_FSG_SUFFIX=/fah_main
SVAP_TURN_HOST=http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081
SVAP_TURN_SUFFIX=/fah_main
SVAP_COR_HOST=http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081
SVAP_COR_SUFFIX=/fah_main
SVAP_PA_HOST=http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081
SVAP_PA_SUFFIX=/fah_main
SVAP_CONS_HOST=http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081
SVAP_CONS_SUFFIX=/fah_main
```

После старта конфигурацию можно менять через `/api/v1/config`. Изменения сохраняются в `app_config`; остальные backend-экземпляры получают обновление через PostgreSQL LISTEN/NOTIFY и дополнительно периодически перечитывают конфигурацию.

## Запуск

Через Docker Compose из корня проекта:

```bash
docker compose up -d --build backend
```

Локально, если Go установлен:

```bash
cd backend
go run ./cmd/server
```

Если Go локально не установлен:

```bash
docker run --rm -v "$PWD:/app" -w /app/backend golang:1.25-alpine go run ./cmd/server
```

## API и авторизация пользователя

Пользователь определяется заголовком:

```http
X-User-ID: test-user
```

Если заголовок отсутствует, пользовательские endpoint'ы возвращают `401`. Если запрос, результат или задача выполнения принадлежат другому пользователю, возвращается `403`.

Основные endpoint'ы:

- `POST /api/v1/queries`
- `GET /api/v1/queries`
- `GET /api/v1/queries/{id}`
- `DELETE /api/v1/queries/{id}`
- `POST /api/v1/query/execute`
- `GET /api/v1/query/executions`
- `GET /api/v1/query/executions/{id}`
- `GET /api/v1/results`
- `DELETE /api/v1/results/{id}`
- `GET /api/v1/results/{id}/data`
- `GET /api/v1/dictionaries`
- `POST /api/v1/dictionaries`
- `GET /api/v1/dictionaries/item`
- `DELETE /api/v1/dictionaries/item`
- `GET /api/v1/analytical-attributes`
- `GET /api/v1/config`
- `PUT /api/v1/config`
- `POST /api/v1/config/apply`
- `GET /api/v1/info`

Swagger:

```text
http://localhost:8080/swagger/
```

## Выполнение запросов

`POST /api/v1/query/execute` принимает сохраненный `query_id` и флаг `async`.

Асинхронный запуск, режим по умолчанию:

```json
{
  "query_id": "371d9a18-dd79-4f30-808b-e60b83cd8f34",
  "async": true,
  "start_rep_date": "2026-01-01",
  "end_rep_date": "2026-03-31",
  "org_code": "6000"
}
```

Ответ `202 Accepted` содержит `execution_id`. Статус можно читать через `/api/v1/query/executions` или `/api/v1/query/executions/{id}`.

Синхронный запуск:

```json
{
  "query_id": "371d9a18-dd79-4f30-808b-e60b83cd8f34",
  "async": false
}
```

При успешном выполнении результат сохраняется в `query_results`, строки - в `query_result_rows`, значения - в `query_row_values`.

## Тесты и Swagger

Запуск тестов:

```bash
docker run --rm -v "$PWD:/app" -w /app/backend golang:1.25-alpine go test ./...
```

Генерация Swagger:

```bash
docker run --rm -v "$PWD:/app" -w /app/backend golang:1.25-alpine \
  go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/server/main.go -o docs
```

Сборка:

```bash
docker compose build backend
```

## Многопользовательская и параллельная работа

- Списки запросов, результатов и задач фильтруются по `X-User-ID`.
- Доступ по UUID дополнительно проверяет владельца.
- Каждый запуск создает отдельную запись в `query_executions`.
- Результаты разных запусков сохраняются под разными `result_id`.
- Сохранение результата выполняется транзакционно: метаданные, строки и значения либо сохраняются вместе, либо не сохраняются.
- Если сохраненный запрос удален во время фонового выполнения, сохранение результата не пройдет по FK, а задача будет помечена как `failed`.
