package ports

import (
	"context"
	"errors"
	"time"

	"svap-query-service/backend/internal/model"
)

// ErrNotFound is the repository-port sentinel for absent persistent entities.
var ErrNotFound = errors.New("not found")

// AnalyticalAttributeRepository is an application port for analytical
// attributes. Implementations belong to infrastructure packages.
type AnalyticalAttributeRepository interface {
	SaveAll(ctx context.Context, attrs []model.AnalyticalAttribute) error
	GetAll(ctx context.Context) ([]model.AnalyticalAttribute, error)
	GetByBusiness(ctx context.Context, business string) ([]model.AnalyticalAttribute, error)
}

// DictionaryCacheRepository is an application port for configuration and
// business dictionary values used by query builders and validation flows.
type DictionaryCacheRepository interface {
	Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error)
	Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error
	GetItem(ctx context.Context, business string, dictionaryCode string, itemCode string) (model.DictionaryItem, error)
	UpsertItem(ctx context.Context, item model.DictionaryItem) (model.DictionaryItem, error)
	DeleteItem(ctx context.Context, business string, dictionaryCode string, itemCode string) error
	Search(ctx context.Context, filter model.DictionaryFilter) ([]model.DictionaryItem, error)
}

// SavedQueryRepository is an application port for saved query templates.
type SavedQueryRepository interface {
	Save(ctx context.Context, q model.SavedQuery) (string, error)
	GetByID(ctx context.Context, id string) (model.SavedQuery, error)
	GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error)
	GetAll(ctx context.Context) ([]model.SavedQuery, error)
	Update(ctx context.Context, q model.SavedQuery) error
	Delete(ctx context.Context, id string) error
}

// QueryResultRepository is an application port for persisted SVAP query
// results, their rows, and row values.
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

// QueryExecutionRepository is an application port for async/sync execution job
// statuses.
type QueryExecutionRepository interface {
	Create(ctx context.Context, execution model.QueryExecution) (model.QueryExecution, error)
	MarkRunning(ctx context.Context, id string, startedAt time.Time) error
	MarkSucceeded(ctx context.Context, id string, resultID string, finishedAt time.Time) error
	MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error
	GetByID(ctx context.Context, id string) (model.QueryExecution, error)
	GetByUserID(ctx context.Context, userID string) ([]model.QueryExecution, error)
}

// ConfigRepository is an application port for persistent runtime configuration.
type ConfigRepository interface {
	Save(ctx context.Context, groupName string, cfg any) error
	Load(ctx context.Context, groupName string) (any, error)
	LoadAll(ctx context.Context) (map[string]any, error)
}
