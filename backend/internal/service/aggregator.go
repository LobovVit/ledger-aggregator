package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"ledger-aggregator/backend/internal/model"
	"ledger-aggregator/backend/internal/repository"
	"ledger-aggregator/backend/internal/svap"
)

type AggregatorService struct {
	svapClient svap.Client
	attrRepo   repository.AnalyticalAttributeRepository
	queryRepo  repository.SavedQueryRepository
	resultRepo repository.QueryResultRepository
	dictRepo   repository.DictionaryCacheRepository
}

func NewAggregatorService(
	client svap.Client,
	attrRepo repository.AnalyticalAttributeRepository,
	queryRepo repository.SavedQueryRepository,
	resultRepo repository.QueryResultRepository,
	dictRepo repository.DictionaryCacheRepository,
) *AggregatorService {
	return &AggregatorService{
		svapClient: client,
		attrRepo:   attrRepo,
		queryRepo:  queryRepo,
		resultRepo: resultRepo,
		dictRepo:   dictRepo,
	}
}

// SyncAnalyticalAttributes синхронизирует аналитические признаки со СВАП
func (s *AggregatorService) SyncAnalyticalAttributes(ctx context.Context) error {
	attrs, err := s.svapClient.GetAnalyticalAttributes(ctx)
	if err != nil {
		return err
	}

	for i := range attrs {
		attrs[i].LastUpdated = time.Now()
	}

	return s.attrRepo.SaveAll(ctx, attrs)
}

// StartSyncJob запускает периодическую синхронизацию
func (s *AggregatorService) StartSyncJob(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := s.SyncAnalyticalAttributes(ctx); err != nil {
					log.Printf("Failed to sync attributes: %v", err)
				}
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

// GetSavedQueries возвращает список сохраненных запросов пользователя
func (s *AggregatorService) GetSavedQueries(ctx context.Context, userID string) ([]model.SavedQuery, error) {
	return s.queryRepo.GetByUserID(ctx, userID)
}

// GetSavedQueryByID возвращает параметры конкретного запроса
func (s *AggregatorService) GetSavedQueryByID(ctx context.Context, id string) (model.SavedQuery, error) {
	return s.queryRepo.GetByID(ctx, id)
}

// CreateSavedQuery сохраняет новый набор параметров
func (s *AggregatorService) CreateSavedQuery(ctx context.Context, q model.SavedQuery) (model.SavedQuery, error) {
	if q.ID == "" {
		q.ID = "uuid-" + strconv.FormatInt(time.Now().UnixNano(), 10) // В реальности UUID
	}
	q.CreatedAt = time.Now()
	err := s.queryRepo.Save(ctx, q)
	return q, err
}

// GetQueryResults возвращает историю выполнений для конкретного сохраненного запроса
func (s *AggregatorService) GetQueryResults(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	return s.resultRepo.GetByQueryID(ctx, queryID)
}

// GetResultData возвращает "плоскую" таблицу данных по ID результата
func (s *AggregatorService) GetResultData(ctx context.Context, resultID string) ([]map[string]any, error) {
	return s.resultRepo.GetFullResultData(ctx, resultID)
}

// ExecuteAndSaveQuery выполняет запрос к СВАП и сохраняет результат в универсальной структуре (строки и значения)
func (s *AggregatorService) ExecuteAndSaveQuery(ctx context.Context, queryID string, offset, limit int) (model.QueryResult, error) {
	savedQuery, err := s.queryRepo.GetByID(ctx, queryID)
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("query not found: %w", err)
	}

	header := model.SVAPHeader{
		Subject:        "SYSTEM", // В реальности из контекста пользователя
		SettlementDate: time.Now(),
		Offset:         offset,
		Limit:          limit,
	}

	// Выполняем запрос через клиент СВАП
	data, err := s.svapClient.ExecuteQuery(ctx, savedQuery.QueryType, header, savedQuery.Params)
	if err != nil {
		return model.QueryResult{}, err
	}

	resultID := "uuid-new-result" // В реальности генерация UUID
	result := model.QueryResult{
		ID:        resultID,
		QueryID:   savedQuery.ID,
		FetchedAt: time.Now(),
	}

	var rows []model.QueryResultRow
	var values []model.QueryResultValue

	// Вспомогательная функция для обработки одной записи (map или структуры)
	processRow := func(rowData any) {
		rowID := "uuid-new-row-" + strconv.Itoa(len(rows))
		rows = append(rows, model.QueryResultRow{
			ID:        rowID,
			ResultID:  resultID,
			CreatedAt: time.Now(),
		})

		// Превращаем rowData (обычно map[string]any из JSON/XML) в набор значений
		if m, ok := rowData.(map[string]any); ok {
			for k, v := range m {
				val := model.QueryResultValue{
					RowID:         rowID,
					AttributeCode: k,
				}
				// Пытаемся определить, число это или строка
				switch tv := v.(type) {
				case float64:
					val.NumericValue = &tv
					val.AttributeValue = fmt.Sprintf("%v", tv)
				case int:
					f := float64(tv)
					val.NumericValue = &f
					val.AttributeValue = strconv.Itoa(tv)
				default:
					val.AttributeValue = fmt.Sprintf("%v", v)
				}
				values = append(values, val)
			}
		}
	}

	// Преобразуем полученные данные в строки
	if dataSlice, ok := data.([]any); ok {
		for _, rowData := range dataSlice {
			processRow(rowData)
		}
	} else if data != nil {
		processRow(data)
	}

	// Сохраняем в БД
	if err := s.resultRepo.Save(ctx, result, rows, values); err != nil {
		return result, err
	}

	return result, nil
}

// GetDictionaryData предоставляет данные справочника с поддержкой кэша
func (s *AggregatorService) GetDictionaryData(ctx context.Context, business, dictCode, query string) ([]model.DictionaryItem, error) {
	// Сначала ищем в локальном кэше
	items, err := s.dictRepo.Search(ctx, business, dictCode, query)
	if err == nil && len(items) > 0 {
		return items, nil
	}

	// Если в кэше нет, тут должна быть логика запроса к ФП (зависит от реализации ФП)
	// Для примера возвращаем пустой список или ошибку
	return nil, nil
}
