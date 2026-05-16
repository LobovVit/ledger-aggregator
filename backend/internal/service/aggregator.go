package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"svap-query-service/backend/internal/model"
	"svap-query-service/backend/internal/ports"

	"github.com/google/uuid"
)

// SVAPClient is the outbound port used by application use cases to call SVAP.
// Infrastructure adapters, such as internal/svap.RealClient, satisfy this
// interface without the service layer importing transport-specific packages.
type SVAPClient interface {
	GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error)
	ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document any) (any, error)
}

// RetentionPolicyProvider is the application port used to resolve result TTL.
type RetentionPolicyProvider interface {
	GetRetentionTTL(userID string, roles []string, orgCode string) time.Duration
}

// AggregatorService contains application use cases for saved queries,
// executions, result persistence, dictionaries, and analytical attributes.
type AggregatorService struct {
	svapClient    SVAPClient
	attrRepo      ports.AnalyticalAttributeRepository
	queryRepo     ports.SavedQueryRepository
	resultRepo    ports.QueryResultRepository
	executionRepo ports.QueryExecutionRepository
	dictRepo      ports.DictionaryCacheRepository
	config        RetentionPolicyProvider
}

// QueryExecutionOptions groups runtime parameters that can override or enrich a
// saved query execution without changing the saved query template.
type QueryExecutionOptions struct {
	QueryID      string
	UserID       string
	Offset       int
	Limit        int
	StartRepDate string
	EndRepDate   string
	Name         string
	Description  string
	Visibility   string
	Roles        []string
	OrgCode      string
}

func NewAggregatorService(
	client SVAPClient,
	attrRepo ports.AnalyticalAttributeRepository,
	queryRepo ports.SavedQueryRepository,
	resultRepo ports.QueryResultRepository,
	dictRepo ports.DictionaryCacheRepository,
	config RetentionPolicyProvider,
	executionRepo ...ports.QueryExecutionRepository,
) *AggregatorService {
	service := &AggregatorService{
		svapClient:    client,
		attrRepo:      attrRepo,
		queryRepo:     queryRepo,
		resultRepo:    resultRepo,
		executionRepo: nil,
		dictRepo:      dictRepo,
		config:        config,
	}
	if len(executionRepo) > 0 {
		service.executionRepo = executionRepo[0]
	}
	return service
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

// GetAnalyticalAttributes возвращает справочник аналитических признаков.
func (s *AggregatorService) GetAnalyticalAttributes(ctx context.Context, business string) ([]model.AnalyticalAttribute, error) {
	if s.attrRepo == nil {
		return nil, errors.New("analytical attribute repository is not configured")
	}
	if business == "" {
		return s.attrRepo.GetAll(ctx)
	}
	return s.attrRepo.GetByBusiness(ctx, business)
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
	if _, err := s.queryRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrSavedQueryNotFound, id)
		}
		return err
	}
	if err := s.queryRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrSavedQueryNotFound, id)
		}
		return err
	}
	return nil
}

// EnsureSavedQueryBelongsToUser checks query ownership before handlers expose or
// mutate query-scoped resources.
func (s *AggregatorService) EnsureSavedQueryBelongsToUser(ctx context.Context, id string, userID string) error {
	query, err := s.GetSavedQueryByID(ctx, id)
	if err != nil {
		return err
	}
	if query.UserID != userID {
		return ErrForbidden
	}
	return nil
}

// EnsureQueryResultBelongsToUser checks ownership through result -> saved query.
func (s *AggregatorService) EnsureQueryResultBelongsToUser(ctx context.Context, id string, userID string) error {
	result, err := s.resultRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrQueryResultNotFound, id)
		}
		return err
	}
	return s.EnsureSavedQueryBelongsToUser(ctx, result.QueryID, userID)
}

// GetSavedQueryByID возвращает параметры конкретного запроса
func (s *AggregatorService) GetSavedQueryByID(ctx context.Context, id string) (model.SavedQuery, error) {
	query, err := s.queryRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return model.SavedQuery{}, fmt.Errorf("%w: %s", ErrSavedQueryNotFound, id)
		}
		return model.SavedQuery{}, err
	}
	return query, nil
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
	if _, err := s.queryRepo.GetByID(ctx, queryID); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrSavedQueryNotFound, queryID)
		}
		return nil, err
	}
	return s.resultRepo.GetByQueryID(ctx, queryID)
}

func (s *AggregatorService) GetQueryResultsByUser(ctx context.Context, userID string) ([]model.QueryResult, error) {
	return s.resultRepo.GetByUserID(ctx, userID)
}

func (s *AggregatorService) GetQueryExecutionsByUser(ctx context.Context, userID string) ([]model.QueryExecution, error) {
	return s.executionRepo.GetByUserID(ctx, userID)
}

func (s *AggregatorService) GetQueryExecutionByID(ctx context.Context, id string) (model.QueryExecution, error) {
	execution, err := s.executionRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return model.QueryExecution{}, fmt.Errorf("%w: %s", ErrQueryExecutionNotFound, id)
		}
		return model.QueryExecution{}, err
	}
	return execution, nil
}

func (s *AggregatorService) DeleteQueryResult(ctx context.Context, id string) error {
	if _, err := s.resultRepo.GetByID(ctx, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrQueryResultNotFound, id)
		}
		return err
	}
	if err := s.resultRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrQueryResultNotFound, id)
		}
		return err
	}
	return nil
}

// GetResultData возвращает "плоскую" таблицу данных по ID результата
func (s *AggregatorService) GetResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error) {
	if _, err := s.resultRepo.GetByID(ctx, resultID); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrQueryResultNotFound, resultID)
		}
		return nil, err
	}
	return s.resultRepo.GetFullResultData(ctx, resultID, offset, limit)
}

func (s *AggregatorService) StartQueryExecutionAsync(ctx context.Context, opts QueryExecutionOptions) (model.QueryExecution, error) {
	execution, err := s.createQueryExecution(ctx, opts, model.QueryExecutionModeAsync)
	if err != nil {
		return model.QueryExecution{}, err
	}

	go func() {
		if _, err := s.runQueryExecution(context.Background(), execution, opts); err != nil {
			log.Printf("failed to execute query %s asynchronously in job %s: %v", opts.QueryID, execution.ID, err)
		}
	}()

	return execution, nil
}

func (s *AggregatorService) RunQueryExecutionSync(ctx context.Context, opts QueryExecutionOptions) (model.QueryExecution, model.QueryResult, error) {
	execution, err := s.createQueryExecution(ctx, opts, model.QueryExecutionModeSync)
	if err != nil {
		return model.QueryExecution{}, model.QueryResult{}, err
	}
	result, err := s.runQueryExecution(ctx, execution, opts)
	if err != nil {
		updatedExecution, getErr := s.GetQueryExecutionByID(ctx, execution.ID)
		if getErr == nil {
			execution = updatedExecution
		}
		return execution, model.QueryResult{}, err
	}
	updatedExecution, err := s.GetQueryExecutionByID(ctx, execution.ID)
	if err == nil {
		execution = updatedExecution
	}
	return execution, result, nil
}

func (s *AggregatorService) createQueryExecution(ctx context.Context, opts QueryExecutionOptions, mode string) (model.QueryExecution, error) {
	savedQuery, err := s.queryRepo.GetByID(ctx, opts.QueryID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return model.QueryExecution{}, fmt.Errorf("%w: %s", ErrSavedQueryNotFound, opts.QueryID)
		}
		return model.QueryExecution{}, err
	}
	if opts.UserID != "" && savedQuery.UserID != opts.UserID {
		return model.QueryExecution{}, ErrForbidden
	}

	execution := model.QueryExecution{
		QueryID:      savedQuery.ID,
		UserID:       savedQuery.UserID,
		Status:       model.QueryExecutionStatusQueued,
		Mode:         mode,
		StartRepDate: firstNonEmpty(opts.StartRepDate, paramString(savedQuery.Params, "StartRepDate")),
		EndRepDate:   firstNonEmpty(opts.EndRepDate, paramString(savedQuery.Params, "EndRepDate")),
		Offset:       opts.Offset,
		Limit:        opts.Limit,
		CreatedAt:    time.Now(),
	}
	return s.executionRepo.Create(ctx, execution)
}

func (s *AggregatorService) runQueryExecution(ctx context.Context, execution model.QueryExecution, opts QueryExecutionOptions) (model.QueryResult, error) {
	startedAt := time.Now()
	if err := s.executionRepo.MarkRunning(ctx, execution.ID, startedAt); err != nil {
		return model.QueryResult{}, err
	}

	result, err := s.ExecuteAndSaveQuery(ctx, opts.QueryID, opts.Offset, opts.Limit, opts.StartRepDate, opts.EndRepDate, opts.Name, opts.Description, opts.Visibility, opts.Roles, opts.OrgCode)
	if err != nil {
		if markErr := s.executionRepo.MarkFailed(ctx, execution.ID, err.Error(), time.Now()); markErr != nil {
			log.Printf("failed to mark query execution %s as failed: %v", execution.ID, markErr)
		}
		return model.QueryResult{}, err
	}

	if err := s.executionRepo.MarkSucceeded(ctx, execution.ID, result.ID, time.Now()); err != nil {
		return model.QueryResult{}, err
	}
	return result, nil
}

// ExecuteAndSaveQuery выполняет запрос к СВАП и сохраняет результат в универсальной структуре (строки и значения).
// Позволяет переопределить StartRepDate, EndRepDate, Name, Description, Visibility.
func (s *AggregatorService) ExecuteAndSaveQuery(ctx context.Context, queryID string, offset, limit int, startRepDate, endRepDate, name, description, visibility string, roles []string, orgCode string) (model.QueryResult, error) {
	savedQuery, err := s.queryRepo.GetByID(ctx, queryID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return model.QueryResult{}, fmt.Errorf("%w: %s", ErrSavedQueryNotFound, queryID)
		}
		return model.QueryResult{}, err
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
		ReqGuid:     uuid.NewString(),
		RequestType: savedQuery.QueryType,
	}

	var document any
	if err := json.Unmarshal([]byte(params), &document); err != nil {
		return model.QueryResult{}, fmt.Errorf("failed to parse saved query params: %w", err)
	}
	document = prepareSVAPDocument(savedQuery.QueryType, document)

	// Выполняем запрос через клиент СВАП
	fullResp, err := s.svapClient.ExecuteQuery(ctx, savedQuery.QueryType, header, document)
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
		if resultBody, ok := respMap["result"].(map[string]any); ok {
			if respData, ok := resultBody["RespData"].([]any); ok {
				dataRows = respData
			} else {
				dataRows = append(dataRows, resultBody)
			}
		} else if doc, ok := respMap["document"].(map[string]any); ok {
			if respData, ok := doc["RespData"].([]any); ok {
				dataRows = respData
			} else if lines, ok := doc["LINES"].([]any); ok {
				dataRows = lines
			} else {
				// Если RespData нет, возможно document сам является списком или объектом
				dataRows = append(dataRows, doc)
			}
		} else if docList, ok := respMap["document"].([]any); ok {
			dataRows = docList
		} else if _, hasHeader := respMap["header"]; hasHeader && len(respMap) == 1 {
			dataRows = []any{}
		} else {
			// Если нет document, возможно данные в корне (OPLIST?)
			dataRows = append(dataRows, respMap)
		}
	} else if respSlice, ok := fullResp.([]any); ok {
		dataRows = respSlice
	} else {
		dataRows = append(dataRows, fullResp)
	}

	result.Meta = mergeResultMeta(result.Meta, map[string]any{
		"QueryID":      savedQuery.ID,
		"StartRepDate": firstNonEmpty(startRepDate, paramString(params, "StartRepDate")),
		"EndRepDate":   firstNonEmpty(endRepDate, paramString(params, "EndRepDate")),
		"Offset":       offset,
		"Limit":        limit,
	})

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

		appendParamValues := func(prefix string, raw any) bool {
			list, ok := raw.([]any)
			if !ok {
				return false
			}
			for _, item := range list {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				pName, _ := itemMap["param"].(string)
				pValue := itemMap["value"]
				if pName == "" {
					continue
				}
				val := model.QueryResultValue{
					RowID:         tempRowID,
					AttributeCode: prefix + pName,
				}
				s.fillValue(&val, pValue)
				values = append(values, val)
			}
			return true
		}

		// Превращаем rowData (обычно map[string]any из JSON/XML) в набор значений
		if m, ok := rowData.(map[string]any); ok {
			for k, v := range m {
				// Обработка VisualParams (список параметров в FSG)
				if k == "VisualParams" {
					appendParamValues("", v)
					continue
				}
				if k == "VisualParams_Dt" || k == "Dt_acc" {
					appendParamValues("Dt.", v)
					continue
				}
				if k == "VisualParams_Cr" || k == "Ct_acc" || k == "Сt_acc" {
					appendParamValues("Ct.", v)
					continue
				}
				if k == "Indicators" {
					if indicators, ok := v.([]any); ok {
						for _, indicator := range indicators {
							if indicatorMap, ok := indicator.(map[string]any); ok {
								code, _ := indicatorMap["data"].(string)
								indicatorValue := indicatorMap["value"]
								if code != "" {
									val := model.QueryResultValue{
										RowID:         tempRowID,
										AttributeCode: code,
									}
									s.fillValue(&val, indicatorValue)
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

func prepareSVAPDocument(queryType string, document any) any {
	docMap, ok := document.(map[string]any)
	if !ok {
		return document
	}
	prepared := make(map[string]any, len(docMap))
	for key, value := range docMap {
		prepared[key] = value
	}
	if queryType == "FSG" || queryType == "TURN" || queryType == "COR" {
		delete(prepared, "ResultParams")
	}
	if queryType == "CONS" {
		delete(prepared, "FilterParams")
	}
	if queryType == "COR" {
		prepared["FilterParams"] = withDefaultSite(prepared["FilterParams"], "-1")
		prepared["VisualParams"] = withDefaultSite(prepared["VisualParams"], "-1")
	}
	return prepared
}

func withDefaultSite(raw any, defaultSite string) any {
	items, ok := raw.([]any)
	if !ok {
		return raw
	}
	normalized := make([]map[string]any, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			normalized = append(normalized, map[string]any{"param": typed, "Site": defaultSite})
		case map[string]any:
			copied := make(map[string]any, len(typed)+1)
			for key, value := range typed {
				copied[key] = value
			}
			if _, ok := copied["Site"]; !ok {
				copied["Site"] = defaultSite
			}
			normalized = append(normalized, copied)
		default:
			normalized = append(normalized, map[string]any{"param": fmt.Sprintf("%v", typed), "Site": defaultSite})
		}
	}
	return normalized
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

func mergeResultMeta(current any, launch map[string]any) map[string]any {
	meta := make(map[string]any, len(launch)+1)
	if currentMap, ok := current.(map[string]any); ok {
		for key, value := range currentMap {
			meta[key] = value
		}
	} else if current != nil {
		meta["svap_meta"] = current
	}
	for key, value := range launch {
		meta[key] = value
	}
	return meta
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func paramString(params string, key string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(params), &parsed); err != nil {
		return ""
	}
	value, ok := parsed[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func (s *AggregatorService) SearchDictionaryItems(ctx context.Context, filter model.DictionaryFilter) ([]model.DictionaryItem, error) {
	return s.dictRepo.Search(ctx, filter)
}

func (s *AggregatorService) GetDictionaryItem(ctx context.Context, business string, dictionaryCode string, itemCode string) (model.DictionaryItem, error) {
	item, err := s.dictRepo.GetItem(ctx, business, dictionaryCode, itemCode)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return model.DictionaryItem{}, fmt.Errorf("%w: %s/%s/%s", ErrDictionaryItemNotFound, business, dictionaryCode, itemCode)
		}
		return model.DictionaryItem{}, err
	}
	return item, nil
}

func (s *AggregatorService) UpsertDictionaryItem(ctx context.Context, item model.DictionaryItem) (model.DictionaryItem, error) {
	return s.dictRepo.UpsertItem(ctx, item)
}

func (s *AggregatorService) DeleteDictionaryItem(ctx context.Context, business string, dictionaryCode string, itemCode string) error {
	if err := s.dictRepo.DeleteItem(ctx, business, dictionaryCode, itemCode); err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return fmt.Errorf("%w: %s/%s/%s", ErrDictionaryItemNotFound, business, dictionaryCode, itemCode)
		}
		return err
	}
	return nil
}
