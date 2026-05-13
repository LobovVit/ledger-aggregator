package repository

import (
	"context"

	"ledger-aggregator/backend/internal/model"
)

// AnalyticalAttributeRepository интерфейс для хранения АП
type AnalyticalAttributeRepository interface {
	SaveAll(ctx context.Context, attrs []model.AnalyticalAttribute) error
	GetAll(ctx context.Context) ([]model.AnalyticalAttribute, error)
	GetByBusiness(ctx context.Context, business string) ([]model.AnalyticalAttribute, error)
}

// DictionaryCacheRepository интерфейс для кэширования справочников ФП
type DictionaryCacheRepository interface {
	Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error)
	Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error
	Search(ctx context.Context, business string, dictionaryCode string, query string) ([]model.DictionaryItem, error)
}

// SavedQueryRepository интерфейс для управления сохраненными запросами
type SavedQueryRepository interface {
	Save(ctx context.Context, q model.SavedQuery) (string, error)
	GetByID(ctx context.Context, id string) (model.SavedQuery, error)
	GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error)
	GetAll(ctx context.Context) ([]model.SavedQuery, error)
	Update(ctx context.Context, q model.SavedQuery) error
	Delete(ctx context.Context, id string) error
}

// QueryResultRepository интерфейс для хранения результатов запросов
type QueryResultRepository interface {
	Save(ctx context.Context, res model.QueryResult, rows []model.QueryResultRow, values []model.QueryResultValue) (string, error)
	GetByQueryID(ctx context.Context, queryID string) ([]model.QueryResult, error)
	GetByUserID(ctx context.Context, userID string) ([]model.QueryResult, error)
	GetByID(ctx context.Context, id string) (model.QueryResult, error)
	GetAll(ctx context.Context) ([]model.QueryResult, error)
	GetRows(ctx context.Context, resultID string) ([]model.QueryResultRow, error)
	GetValuesByRowID(ctx context.Context, rowID string) ([]model.QueryResultValue, error)
	GetFullResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error)
	Delete(ctx context.Context, id string) error
}

// ConfigRepository интерфейс для персистентного хранения конфигурации
type ConfigRepository interface {
	Save(ctx context.Context, groupName string, cfg any) error
	Load(ctx context.Context, groupName string) (any, error)
	LoadAll(ctx context.Context) (map[string]any, error)
}
