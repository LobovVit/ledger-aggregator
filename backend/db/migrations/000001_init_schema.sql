-- Схема базы данных для подсистемы агрегатора СВАП

-- 1. Таблица аналитических признаков
CREATE TABLE IF NOT EXISTS analytical_attributes (
    code VARCHAR(50) PRIMARY KEY,
    name TEXT NOT NULL,
    businesses TEXT[] NOT NULL,
    in_account BOOLEAN DEFAULT FALSE,
    use_in_balance BOOLEAN DEFAULT FALSE,
    validation_type VARCHAR(20), -- Service, RegExpression, List
    validation_value TEXT,
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. Кэш справочников функциональных подсистем
CREATE TABLE IF NOT EXISTS dictionaries_cache (
    id SERIAL PRIMARY KEY,
    business VARCHAR(10) NOT NULL,
    dictionary_code VARCHAR(50) NOT NULL,
    item_code VARCHAR(100) NOT NULL,
    item_name TEXT NOT NULL,
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(business, dictionary_code, item_code)
);

CREATE INDEX idx_dict_search ON dictionaries_cache(business, dictionary_code, item_code);

-- 3. Сохраненные запросы пользователей
CREATE TABLE IF NOT EXISTS saved_queries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(50) NOT NULL,
    system_code VARCHAR(20) NOT NULL, -- ГК, ЛС, ЖО и т.д.
    query_type VARCHAR(20) NOT NULL, -- TURN, FSG, PA...
    params JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 4. Результаты выполнения запросов (метаданные)
CREATE TABLE IF NOT EXISTS query_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    query_id UUID NOT NULL REFERENCES saved_queries(id),
    meta JSONB, -- Метаданные для АИ
    fetched_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 5. Строки результатов выполнения запросов (сущности)
CREATE TABLE IF NOT EXISTS query_result_rows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    result_id UUID NOT NULL REFERENCES query_results(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 6. Значения измерений и показателей для строк (Универсальная структура)
CREATE TABLE IF NOT EXISTS query_row_values (
    id SERIAL PRIMARY KEY,
    row_id UUID NOT NULL REFERENCES query_result_rows(id) ON DELETE CASCADE,
    attribute_code VARCHAR(50) NOT NULL, -- Ссылка на analytical_attributes или код поля из СВАП
    attribute_value TEXT,                -- Значение в текстовом виде для универсальности
    numeric_value NUMERIC,               -- Отдельное поле для числовых данных (суммы, обороты)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_results_query ON query_results(query_id);
CREATE INDEX idx_rows_result ON query_result_rows(result_id);
CREATE INDEX idx_values_row ON query_row_values(row_id);
CREATE INDEX idx_values_attr ON query_row_values(attribute_code);
