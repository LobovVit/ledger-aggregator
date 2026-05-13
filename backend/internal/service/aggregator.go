package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"ledger-aggregator/backend/internal/model"
	"ledger-aggregator/backend/internal/repository"
	"ledger-aggregator/backend/internal/svap"

	"github.com/google/uuid"
)

type AggregatorService struct {
	svapClient svap.Client
	attrRepo   repository.AnalyticalAttributeRepository
	queryRepo  repository.SavedQueryRepository
	resultRepo repository.QueryResultRepository
	dictRepo   repository.DictionaryCacheRepository
	config     interface {
		GetRetentionTTL(userID string, roles []string, orgCode string) time.Duration
	}
}

func NewAggregatorService(
	client svap.Client,
	attrRepo repository.AnalyticalAttributeRepository,
	queryRepo repository.SavedQueryRepository,
	resultRepo repository.QueryResultRepository,
	dictRepo repository.DictionaryCacheRepository,
	config interface {
		GetRetentionTTL(userID string, roles []string, orgCode string) time.Duration
	},
) *AggregatorService {
	return &AggregatorService{
		svapClient: client,
		attrRepo:   attrRepo,
		queryRepo:  queryRepo,
		resultRepo: resultRepo,
		dictRepo:   dictRepo,
		config:     config,
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
	if userID == "" {
		return s.queryRepo.GetAll(ctx)
	}
	return s.queryRepo.GetByUserID(ctx, userID)
}

func (s *AggregatorService) DeleteSavedQuery(ctx context.Context, id string) error {
	return s.queryRepo.Delete(ctx, id)
}

// GetSavedQueryByID возвращает параметры конкретного запроса
func (s *AggregatorService) GetSavedQueryByID(ctx context.Context, id string) (model.SavedQuery, error) {
	return s.queryRepo.GetByID(ctx, id)
}

// CreateSavedQuery сохраняет новый набор параметров
func (s *AggregatorService) CreateSavedQuery(ctx context.Context, q model.SavedQuery) (model.SavedQuery, error) {
	q.CreatedAt = time.Now()
	if q.Visibility == "" {
		q.Visibility = model.VisibilityPrivate
	}
	id, err := s.queryRepo.Save(ctx, q)
	if err != nil {
		log.Printf("Error saving query: %v", err)
		return q, err
	}
	q.ID = id
	return q, nil
}

// GetQueryResults возвращает историю выполнений для конкретного сохраненного запроса
func (s *AggregatorService) GetQueryResults(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	if queryID == "" {
		return s.resultRepo.GetAll(ctx)
	}
	return s.resultRepo.GetByQueryID(ctx, queryID)
}

func (s *AggregatorService) GetQueryResultsByUser(ctx context.Context, userID string) ([]model.QueryResult, error) {
	return s.resultRepo.GetByUserID(ctx, userID)
}

func (s *AggregatorService) DeleteQueryResult(ctx context.Context, id string) error {
	return s.resultRepo.Delete(ctx, id)
}

// GetResultData возвращает "плоскую" таблицу данных по ID результата
func (s *AggregatorService) GetResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error) {
	return s.resultRepo.GetFullResultData(ctx, resultID, offset, limit)
}

// ExecuteAndSaveQuery выполняет запрос к СВАП и сохраняет результат в универсальной структуре (строки и значения).
// Позволяет переопределить StartRepDate, EndRepDate, Name, Description, Visibility.
func (s *AggregatorService) ExecuteAndSaveQuery(ctx context.Context, queryID string, offset, limit int, startRepDate, endRepDate, name, description, visibility string, roles []string, orgCode string) (model.QueryResult, error) {
	savedQuery, err := s.queryRepo.GetByID(ctx, queryID)
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("query not found: %w", err)
	}

	params := savedQuery.Params
	// Переопределение дат, если они переданы
	if startRepDate != "" || endRepDate != "" {
		var pMap map[string]any
		if err := json.Unmarshal([]byte(params), &pMap); err == nil {
			if startRepDate != "" {
				pMap["StartRepDate"] = startRepDate
			}
			if endRepDate != "" {
				pMap["EndRepDate"] = endRepDate
			}
			newParams, err := json.Marshal(pMap)
			if err == nil {
				params = string(newParams)
			}
		}
	}

	header := model.SVAPHeader{
		Source:      "RMS",
		ReqDateTime: model.SVAPTime(time.Now()),
		Version:     "1.0",
		ReqGuid:     "gen-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		RequestType: savedQuery.QueryType,
	}

	// Добавляем пагинацию только если она задана (не 0)
	if offset > 0 {
		header.Offset = offset
	}
	if limit > 0 {
		header.Limit = limit
	}

	// Выполняем запрос через клиент СВАП
	fullResp, err := s.svapClient.ExecuteQuery(ctx, savedQuery.QueryType, header, params)
	if err != nil {
		return model.QueryResult{}, err
	}

	fetchedAt := time.Now()
	result := model.QueryResult{
		QueryID:     savedQuery.ID,
		Name:        name,
		Description: description,
		Visibility:  visibility,
		FetchedAt:   fetchedAt,
	}

	// Если значения не заданы, берем из выполняемого запроса
	if result.Name == "" {
		result.Name = savedQuery.Name
	}
	if result.Description == "" {
		result.Description = savedQuery.Description
	}
	if result.Visibility == "" {
		result.Visibility = savedQuery.Visibility
	}

	// Расчет TTL
	if s.config != nil {
		ttl := s.config.GetRetentionTTL(savedQuery.UserID, roles, orgCode)
		expiresAt := fetchedAt.Add(ttl)
		result.ExpiresAt = &expiresAt
	}

	var dataRows []any
	// Разбираем структуру ответа СВАП
	if respMap, ok := fullResp.(map[string]any); ok {
		// Сохраняем meta если есть
		if meta, ok := respMap["meta"]; ok {
			result.Meta = meta
		}

		// Извлекаем данные из document.RespData (стандарт для FSG/TURN)
		if doc, ok := respMap["document"].(map[string]any); ok {
			if respData, ok := doc["RespData"].([]any); ok {
				dataRows = respData
			} else {
				// Если RespData нет, возможно document сам является списком или объектом
				dataRows = append(dataRows, doc)
			}
		} else if docList, ok := respMap["document"].([]any); ok {
			dataRows = docList
		} else {
			// Если нет document, возможно данные в корне (OPLIST?)
			dataRows = append(dataRows, respMap)
		}
	} else if respSlice, ok := fullResp.([]any); ok {
		dataRows = respSlice
	} else {
		dataRows = append(dataRows, fullResp)
	}

	var rows []model.QueryResultRow
	var values []model.QueryResultValue

	// Вспомогательная функция для обработки одной записи (map или структуры)
	processRow := func(rowData any) {
		// Используем временный ID для связки строки и ее значений в памяти
		tempRowID := uuid.NewString()
		rows = append(rows, model.QueryResultRow{
			ID:        tempRowID,
			CreatedAt: time.Now(),
		})

		// Превращаем rowData (обычно map[string]any из JSON/XML) в набор значений
		if m, ok := rowData.(map[string]any); ok {
			for k, v := range m {
				// Обработка VisualParams (список параметров в FSG)
				if k == "VisualParams" {
					if vpList, ok := v.([]any); ok {
						for _, vp := range vpList {
							if vpMap, ok := vp.(map[string]any); ok {
								pName, _ := vpMap["param"].(string)
								pValue := vpMap["value"]
								if pName != "" {
									val := model.QueryResultValue{
										RowID:         tempRowID,
										AttributeCode: pName,
									}
									s.fillValue(&val, pValue)
									values = append(values, val)
								}
							}
						}
					}
					continue
				}

				val := model.QueryResultValue{
					RowID:         tempRowID,
					AttributeCode: k,
				}
				s.fillValue(&val, v)
				values = append(values, val)
			}
		}
	}

	// Преобразуем полученные данные в строки
	for _, rowData := range dataRows {
		processRow(rowData)
	}

	// Сохраняем в БД
	finalResultID, err := s.resultRepo.Save(ctx, result, rows, values)
	if err != nil {
		return result, err
	}
	result.ID = finalResultID

	return result, nil
}

func (s *AggregatorService) fillValue(val *model.QueryResultValue, v any) {
	switch tv := v.(type) {
	case float64:
		val.NumericValue = &tv
		val.AttributeValue = fmt.Sprintf("%v", tv)
	case int:
		f := float64(tv)
		val.NumericValue = &f
		val.AttributeValue = strconv.Itoa(tv)
	case nil:
		val.AttributeValue = ""
	default:
		val.AttributeValue = fmt.Sprintf("%v", v)
	}
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
	return []model.DictionaryItem{}, nil
}
