# SVAP Query Service

SVAP Query Service - сервис для создания, хранения и выполнения запросов к СВАП с web-интерфейсом конструктора запросов. Проект состоит из backend на Go, frontend на Vite и PostgreSQL.

## Что умеет система

- Конструктор запросов FSG, TURN, COR, PA и CONS.
- Синхронный и асинхронный запуск сохраненных запросов.
- Статусная модель выполнений: `queued`, `running`, `succeeded`, `failed`.
- Хранение результатов в универсальной структуре `query_results`, `query_result_rows`, `query_row_values`.
- Справочники и аналитические признаки для построения условий, колонок и индикаторов.
- Динамическая конфигурация адресов СВАП через API и PostgreSQL LISTEN/NOTIFY.
- Swagger-документация backend API.
- Docker Compose для запуска PostgreSQL, backend и frontend вместе.

## Структура

```text
.
├── backend/                 # Go API, сервисный слой, репозитории, миграции, Swagger
├── frontend/                # Vite UI для работы со всеми endpoint
├── documents/               # Исходная документация по форматам запросов
├── docker-compose.yaml      # Совместный запуск БД, backend и frontend
├── .env.example             # Пример переменных окружения для compose
└── Makefile                 # Базовые команды backend
```

Подробности:

- [backend/README.md](backend/README.md)
- [frontend/README.md](frontend/README.md)

## Быстрый запуск

```bash
docker compose up -d --build
```

После запуска:

- frontend: http://localhost:5173
- backend API: http://localhost:8080/api/v1
- Swagger: http://localhost:8080/swagger/
- PostgreSQL: `localhost:5432`, БД `svap_query_service`

Проверить контейнеры:

```bash
docker compose ps
docker compose logs -f backend
```

Остановить:

```bash
docker compose down
```

Удалить данные БД и начать с чистой установки:

```bash
docker compose down -v
docker compose up -d --build
```

## Конфигурация

Основные переменные задаются в `docker-compose.yaml` или через окружение.

По умолчанию все типы запросов используют:

- host: `http://fk-eb-svap-dev-fb-svip-datamart.otr.ru:8081`
- suffix: `/fah_main`

Переменные для переопределения:

```bash
SVAP_FSG_HOST
SVAP_FSG_SUFFIX
SVAP_TURN_HOST
SVAP_TURN_SUFFIX
SVAP_COR_HOST
SVAP_COR_SUFFIX
SVAP_PA_HOST
SVAP_PA_SUFFIX
SVAP_CONS_HOST
SVAP_CONS_SUFFIX
SVAP_TIMEOUT_SECONDS
SVAP_INSECURE_SKIP_VERIFY
```

Backend также поддерживает динамическую конфигурацию через endpoint'ы `/api/v1/config`. Изменения сохраняются в БД и рассылаются другим экземплярам приложения через PostgreSQL NOTIFY.

## Миграции

Миграции находятся в `backend/db/migrations` и рассчитаны на установку на чистую БД. При старте backend автоматически выполняет новые SQL-файлы и записывает их в `schema_migrations`.

Текущие файлы:

- `000001_init_schema.sql` - схема БД и базовые таблицы.
- `000002_config_data.sql` - конфигурационные справочники интерфейса.
- `000003_load_data.sql` - загрузка бизнес-справочников из TSV-файла.

Целевая схема задается переменной `DB_SCHEMA`. SQL-файлы не должны жестко переключать `search_path`; это делает migration runner.

Для многоконтейнерного запуска используется PostgreSQL advisory lock, поэтому при старте нескольких backend-экземпляров миграции выполняет только один из них.

## Пользовательский контекст

Все пользовательские endpoint'ы требуют заголовок:

```http
X-User-ID: test-user
```

Backend проверяет владельца сохраненного запроса, результата и задачи выполнения. Доступ по известному UUID другого пользователя возвращает `403 Forbidden`.

## Основные API

- `POST /api/v1/queries` - создать сохраненный запрос.
- `GET /api/v1/queries` - список запросов текущего пользователя.
- `GET /api/v1/queries/{id}` - получить запрос.
- `DELETE /api/v1/queries/{id}` - удалить запрос.
- `POST /api/v1/query/execute` - выполнить запрос синхронно или асинхронно.
- `GET /api/v1/query/executions` - список задач выполнения пользователя.
- `GET /api/v1/query/executions/{id}` - статус задачи.
- `GET /api/v1/results` - результаты пользователя.
- `GET /api/v1/results/{id}/data` - строки результата из `query_row_values`.
- `GET /api/v1/dictionaries` - поиск по справочникам.
- `GET /api/v1/analytical-attributes` - аналитические признаки.
- `GET /api/v1/config` - текущая подготовленная конфигурация.

Полная спецификация доступна в Swagger: http://localhost:8080/swagger/

## Разработка

Backend tests:

```bash
docker run --rm -v "$PWD:/app" -w /app/backend golang:1.25-alpine go test ./...
```

Frontend build:

```bash
cd frontend
npm install
npm run build
```

Пересборка compose:

```bash
docker compose up -d --build backend frontend
```

## Масштабирование

Несколько backend-экземпляров могут работать с одной БД:

- миграции сериализуются advisory lock'ом;
- конфигурация синхронизируется через LISTEN/NOTIFY и периодический reload;
- результаты и задачи выполнения изолированы по UUID и `user_id`;
- проверки доступа выполняются не только в списках, но и при чтении/удалении/запуске по UUID.

В текущем `docker-compose.yaml` backend публикуется как `8080:8080`, поэтому прямой `docker compose --scale backend=2` упрется в конфликт host-порта. Для реального масштабирования нужен reverse proxy/load balancer или запуск дополнительных backend без публикации порта наружу.
