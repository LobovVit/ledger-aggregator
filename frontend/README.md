# Frontend

Frontend SVAP Query Service - Vite-приложение без отдельного backend-for-frontend. UI напрямую вызывает REST API backend через `/api/v1`.

## Возможности

- Рабочие разделы для FSG, TURN, COR, PA и CONS.
- Список сохраненных запросов в левой панели по каждому типу.
- Создание запросов из условий выборки, колонок, индикаторов и параметров запуска.
- Drag-and-drop порядок колонок.
- Добавление аналитических признаков и индикаторов из справочников БД.
- Выбор значений условий из связанных справочников.
- Синхронный или асинхронный запуск запроса.
- Таблица запусков и результатов по выбранному запросу.
- Просмотр строк успешного результата из `query_row_values`.
- Разделы для всех основных API: запросы, задачи, результаты, справочники, конфигурация, info.

## Требования

- Node.js 24 или совместимая версия.
- Запущенный backend на `http://localhost:8080`.

При запуске через Docker Compose frontend доступен на `http://localhost:5173`, а nginx проксирует `/api/v1` и `/swagger` в backend.

## Запуск

Из каталога `frontend`:

```bash
npm install
npm run dev
```

Открыть:

```text
http://localhost:5173
```

Production build:

```bash
npm run build
```

Preview:

```bash
npm run preview
```

Через Docker Compose из корня проекта:

```bash
docker compose up -d --build frontend
```

## Настройки UI

В интерфейсе настройки API base и `X-User-ID` вынесены в раздел `Info`.

Значения сохраняются в `localStorage`:

- `svap-ui-api-base`, по умолчанию `/api/v1`;
- `svap-ui-user-id`, по умолчанию `test-user`.

Backend требует заголовок `X-User-ID` для пользовательских endpoint'ов. Frontend автоматически подставляет его во все запросы.

## Источники данных UI

Frontend не содержит fallback-констант для операций, типов запросов, колонок и индикаторов. Интерфейс считает, что БД доступна и заполнена миграциями.

Основные справочники:

- `SVAP_QUERY_TYPES` - типы запросов: FSG, TURN, COR, PA, CONS.
- `SVAP_QUERY_OPERATIONS` - допустимые операции условий.
- `SVAP_QUERY_FILTER_<TYPE>` - допустимые аналитические признаки условий.
- `SVAP_QUERY_VISUAL_<TYPE>` - допустимые аналитические признаки колонок.
- `SVAP_QUERY_INDICATORS_<TYPE>` - индикаторы PA/CONS и результатные поля для FSG/TURN/COR.
- бизнес-справочники, связанные с `analytical_attribute_code`, используются для выбора значений условий.

Данные загружаются через:

- `GET /api/v1/dictionaries`
- `GET /api/v1/analytical-attributes`
- `GET /api/v1/queries`
- `GET /api/v1/query/executions`
- `GET /api/v1/results`

## Структура

```text
frontend/
├── src/main.js       # состояние UI, вызовы API, render-функции
├── src/styles.css    # оформление
├── index.html
├── nginx.conf        # production proxy для compose
├── Dockerfile
├── package.json
└── vite.config.js
```

## Проверка

```bash
npm run build
```

При изменении backend API стоит дополнительно проверить UI в браузере:

- открыть `http://localhost:5173`;
- перейти в FSG/TURN/COR/PA/CONS;
- выбрать или создать сохраненный запрос;
- запустить async и sync;
- открыть успешный результат и проверить таблицу строк.
