-- Итоговая схема базы данных для чистой установки SVAP Query Service.
-- Целевая схема создается migration runner'ом и задается через DB_SCHEMA.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 1. Таблица аналитических признаков
CREATE TABLE IF NOT EXISTS analytical_attributes (
    code VARCHAR(50) PRIMARY KEY,
    name TEXT NOT NULL,
    businesses TEXT[] NOT NULL,
    in_account BOOLEAN NOT NULL DEFAULT FALSE,
    use_in_balance BOOLEAN NOT NULL DEFAULT FALSE,
    validation_type VARCHAR(20),
    validation_value TEXT,
    last_updated TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE analytical_attributes IS 'Справочник аналитических признаков из СВАП';
COMMENT ON COLUMN analytical_attributes.validation_type IS 'Тип валидации: Service, RegExpression, List';

INSERT INTO analytical_attributes (code, name, businesses, in_account, use_in_balance, validation_type, validation_value)
VALUES
    ('budgetCode', 'Бюджет', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9]{7,8}$'),
    ('kbk', 'КБК', ARRAY['FB', 'ФБ'], TRUE, TRUE, 'RegExpression', '^[0-9%]{1,20}$'),
    ('account', 'Бух счет', ARRAY['FB', 'ФБ'], TRUE, TRUE, 'RegExpression', '^[0-9]{5}$'),
    ('pa', 'Лицевой счет', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9]{1,20}$'),
    ('ls', 'ЛС', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9]{1,20}$'),
    ('faip', 'ФАИП', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9]{1,20}$'),
    ('targetCode', 'Код цели', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9%]{1,20}$'),
    ('currencyCode', 'Код валюты', ARRAY['FB', 'ФБ'], FALSE, TRUE, 'RegExpression', '^[0-9]{3}$'),
    ('tofk', 'ТОФК', ARRAY['FB', 'ФБ'], FALSE, FALSE, 'RegExpression', '^[0-9]{4}$'),
    ('contourCode', 'Код контура', ARRAY['FB', 'ФБ'], FALSE, FALSE, NULL, NULL),
    ('documentId', 'Номер БО', ARRAY['FB', 'ФБ'], FALSE, FALSE, NULL, NULL),
    ('oktmo', 'ОКТМО', ARRAY['FB', 'ФБ'], FALSE, FALSE, 'RegExpression', '^[0-9]{1,11}$'),
    ('subdep', 'Подвед', ARRAY['FB', 'ФБ'], FALSE, FALSE, NULL, NULL),
    ('secret', 'Признак секретности', ARRAY['FB', 'ФБ'], FALSE, FALSE, NULL, NULL)
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    businesses = EXCLUDED.businesses,
    in_account = EXCLUDED.in_account,
    use_in_balance = EXCLUDED.use_in_balance,
    validation_type = EXCLUDED.validation_type,
    validation_value = EXCLUDED.validation_value,
    last_updated = CURRENT_TIMESTAMP;

-- 2. Кэш справочников функциональных подсистем
CREATE TABLE IF NOT EXISTS dictionaries_cache (
    id SERIAL PRIMARY KEY,
    business VARCHAR(10) NOT NULL,
    dictionary_code VARCHAR(50) NOT NULL,
    item_code VARCHAR(100) NOT NULL,
    item_short_name TEXT NOT NULL,
    item_full_name TEXT NOT NULL,
    analytical_attribute_code VARCHAR(50) REFERENCES analytical_attributes(code) ON DELETE SET NULL,
    last_updated TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(business, dictionary_code, item_code)
);

CREATE INDEX IF NOT EXISTS idx_dict_search
    ON dictionaries_cache(business, dictionary_code, item_code);
CREATE INDEX IF NOT EXISTS idx_dict_attribute
    ON dictionaries_cache(analytical_attribute_code);

-- 3. Сохраненные запросы пользователей
CREATE TABLE IF NOT EXISTS saved_queries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    visibility VARCHAR(20) NOT NULL DEFAULT 'private',
    query_type VARCHAR(20) NOT NULL,
    params JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT saved_queries_visibility_check
        CHECK (visibility IN ('private', 'organization', 'public'))
);

COMMENT ON COLUMN saved_queries.visibility IS 'Видимость запроса: private, organization, public';
COMMENT ON COLUMN saved_queries.query_type IS 'Тип запроса: OPLIST, FSG, TURN, ReportGK, COR, PA, CONS';

CREATE INDEX IF NOT EXISTS idx_saved_queries_user
    ON saved_queries(user_id, created_at DESC);

-- 4. Результаты выполнения запросов (метаданные)
CREATE TABLE IF NOT EXISTS query_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_id UUID NOT NULL REFERENCES saved_queries(id) ON DELETE CASCADE,
    name VARCHAR(255),
    description TEXT,
    visibility VARCHAR(20),
    meta JSONB,
    fetched_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT query_results_visibility_check
        CHECK (visibility IS NULL OR visibility IN ('private', 'organization', 'public'))
);

COMMENT ON COLUMN query_results.name IS 'Название результата (копия из запроса или переопределенное)';
COMMENT ON COLUMN query_results.description IS 'Описание результата';
COMMENT ON COLUMN query_results.visibility IS 'Видимость результата: private, organization, public';
COMMENT ON COLUMN query_results.meta IS 'Метаинформация для АИ';
COMMENT ON COLUMN query_results.expires_at IS 'Дата и время, после которых результат может быть удален';

CREATE INDEX IF NOT EXISTS idx_results_query
    ON query_results(query_id);
CREATE INDEX IF NOT EXISTS idx_results_expires_at
    ON query_results(expires_at);

-- 5. Задачи выполнения запросов
CREATE TABLE IF NOT EXISTS query_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_id UUID NOT NULL REFERENCES saved_queries(id) ON DELETE CASCADE,
    user_id VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'queued',
    mode VARCHAR(20) NOT NULL DEFAULT 'async',
    start_rep_date VARCHAR(10),
    end_rep_date VARCHAR(10),
    offset_rows INTEGER NOT NULL DEFAULT 0,
    limit_rows INTEGER NOT NULL DEFAULT 0,
    result_id UUID REFERENCES query_results(id) ON DELETE SET NULL,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP WITH TIME ZONE,
    finished_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT query_executions_status_check
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    CONSTRAINT query_executions_mode_check
        CHECK (mode IN ('sync', 'async'))
);

CREATE INDEX IF NOT EXISTS idx_query_executions_user
    ON query_executions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_query_executions_query
    ON query_executions(query_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_query_executions_status
    ON query_executions(status);

-- 6. Строки результатов выполнения запросов (сущности)
CREATE TABLE IF NOT EXISTS query_result_rows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    result_id UUID NOT NULL REFERENCES query_results(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_rows_result
    ON query_result_rows(result_id);

-- 7. Значения измерений и показателей для строк
CREATE TABLE IF NOT EXISTS query_row_values (
    id SERIAL PRIMARY KEY,
    row_id UUID NOT NULL REFERENCES query_result_rows(id) ON DELETE CASCADE,
    attribute_code VARCHAR(50) NOT NULL,
    attribute_value TEXT,
    numeric_value NUMERIC,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON COLUMN query_row_values.attribute_code IS 'Ссылка на analytical_attributes или код поля из СВАП';
COMMENT ON COLUMN query_row_values.attribute_value IS 'Значение в текстовом виде для универсальности';
COMMENT ON COLUMN query_row_values.numeric_value IS 'Отдельное поле для числовых данных';

CREATE INDEX IF NOT EXISTS idx_values_row
    ON query_row_values(row_id);
CREATE INDEX IF NOT EXISTS idx_values_attr
    ON query_row_values(attribute_code);

-- 8. Динамическая конфигурация приложения
CREATE TABLE IF NOT EXISTS app_config (
    group_name VARCHAR(50) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE OR REPLACE FUNCTION notify_config_update()
RETURNS TRIGGER AS $$
BEGIN
  PERFORM pg_notify('config_updated', NEW.group_name);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_config_update ON app_config;
CREATE TRIGGER trg_config_update
AFTER INSERT OR UPDATE ON app_config
FOR EACH ROW EXECUTE FUNCTION notify_config_update();
