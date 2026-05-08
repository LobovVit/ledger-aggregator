-- Миграция для хранения конфигурации и поддержки многоузловой работы
CREATE TABLE IF NOT EXISTS app_config (
    group_name VARCHAR(50) PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Функция для уведомления об изменении конфигурации
CREATE OR REPLACE FUNCTION notify_config_update()
RETURNS TRIGGER AS $$
BEGIN
  PERFORM pg_notify('config_updated', NEW.group_name);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Триггер для автоматического уведомления
DROP TRIGGER IF EXISTS trg_config_update ON app_config;
CREATE TRIGGER trg_config_update
AFTER INSERT OR UPDATE ON app_config
FOR EACH ROW EXECUTE FUNCTION notify_config_update();

-- Начальная конфигурация (если таблицы пусты)
-- INSERT INTO app_config (key, value) VALUES ('main', '{"server_port": "8080", "svap": {"gk_host": "", "ls_host": "", "jo_host": ""}}') ON CONFLICT DO NOTHING;
