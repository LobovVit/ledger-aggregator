package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"svap-query-service/backend/internal/model"
	"svap-query-service/backend/internal/repository"
	"svap-query-service/backend/internal/service"
)

const missingID = "371d9a18-dd79-4f30-808b-e60b83cd8f34"

func TestExecuteQueryMissingSavedQueryReturnsNotFound(t *testing.T) {
	handler := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"`+missingID+`"}`))
	setUser(req, "test-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusNotFound)
	assertBodyContains(t, rec, "saved query with id "+missingID+" was not found")
}

func TestExecuteQueryInvalidIDReturnsBadRequest(t *testing.T) {
	handler := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"bad-id"}`))
	setUser(req, "test-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusBadRequest)
	assertBodyContains(t, rec, "invalid query_id")
}

func TestExecuteQueryReturnsAcceptedAndRunsInBackground(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "test-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	resultRepo := &fakeQueryResultRepo{}
	executionRepo := newFakeQueryExecutionRepo()
	executionRepo.succeededCh = make(chan struct{})
	handler := newTestHandlerWithRepos(queryRepo, resultRepo, executionRepo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"`+missingID+`"}`))
	setUser(req, "test-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	assertBodyContains(t, rec, `"status":"accepted"`)
	assertBodyContains(t, rec, `"query_id":"`+missingID+`"`)
	assertBodyContains(t, rec, `"execution_id":"`)

	select {
	case <-executionRepo.succeededCh:
	case <-time.After(time.Second):
		t.Fatal("background query execution did not mark the job as succeeded")
	}

	executions, err := executionRepo.GetByUserID(context.Background(), "test-user")
	if err != nil {
		t.Fatalf("GetByUserID() error = %v", err)
	}
	if len(executions) != 1 || executions[0].Status != model.QueryExecutionStatusSucceeded {
		t.Fatalf("execution status = %#v, want one succeeded execution", executions)
	}
	if executions[0].ResultID == "" {
		t.Fatalf("execution result_id is empty: %#v", executions[0])
	}
}

func TestExecuteQueryCanRunSynchronously(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "test-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	resultRepo := &fakeQueryResultRepo{}
	handler := newTestHandlerWithRepos(queryRepo, resultRepo, newFakeQueryExecutionRepo())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"`+missingID+`","async":false}`))
	setUser(req, "test-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusOK)
	assertBodyContains(t, rec, `"status":"succeeded"`)
	assertBodyContains(t, rec, `"result":`)
}

func TestExecuteQuerySyncFailureMarksExecutionFailed(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "test-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	executionRepo := newFakeQueryExecutionRepo()
	handler := newTestHandlerWithClient(&fakeSVAPClient{executeErr: errors.New("SVAP unavailable")}, queryRepo, &fakeQueryResultRepo{}, executionRepo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"`+missingID+`","async":false}`))
	setUser(req, "test-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusInternalServerError)
	assertBodyContains(t, rec, "failed to execute query")

	execution, err := executionRepo.GetByID(context.Background(), executionRepo.nextID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if execution.Status != model.QueryExecutionStatusFailed {
		t.Fatalf("execution status = %q, want failed", execution.Status)
	}
	if !strings.Contains(execution.ErrorMessage, "SVAP unavailable") {
		t.Fatalf("execution error = %q, want SVAP unavailable", execution.ErrorMessage)
	}
}

func TestExecuteQueryRejectsOtherUser(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "owner-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	executionRepo := newFakeQueryExecutionRepo()
	handler := newTestHandlerWithRepos(queryRepo, &fakeQueryResultRepo{}, executionRepo)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/query/execute", strings.NewReader(`{"query_id":"`+missingID+`"}`))
	setUser(req, "another-user")
	rec := httptest.NewRecorder()

	handler.ExecuteQuery(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
	assertBodyContains(t, rec, "saved query belongs to another user")
	if len(executionRepo.executions) != 0 {
		t.Fatalf("created executions = %#v, want none", executionRepo.executions)
	}
}

func TestGetQueryExecutionRejectsOtherUser(t *testing.T) {
	executionRepo := newFakeQueryExecutionRepo()
	_, err := executionRepo.Create(context.Background(), model.QueryExecution{
		ID:      executionRepo.nextID,
		QueryID: missingID,
		UserID:  "owner-user",
		Status:  model.QueryExecutionStatusSucceeded,
		Mode:    model.QueryExecutionModeSync,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	handler := newTestHandlerWithRepos(&fakeSavedQueryRepo{}, &fakeQueryResultRepo{}, executionRepo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query/executions/"+executionRepo.nextID, nil)
	req.Header.Set("X-User-ID", "another-user")
	req.SetPathValue("id", executionRepo.nextID)
	rec := httptest.NewRecorder()

	handler.GetQueryExecution(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
	assertBodyContains(t, rec, "query execution belongs to another user")
}

func TestListQueryResultsMissingSavedQueryReturnsNotFound(t *testing.T) {
	handler := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/queries/"+missingID+"/results", nil)
	setUser(req, "test-user")
	req.SetPathValue("id", missingID)
	rec := httptest.NewRecorder()

	handler.ListQueryResults(rec, req)

	assertStatus(t, rec, http.StatusNotFound)
	assertBodyContains(t, rec, "saved query with id "+missingID+" was not found")
}

func TestQueryEndpointsRejectOtherUser(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "owner-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	handler := newTestHandlerWithRepos(queryRepo, &fakeQueryResultRepo{}, newFakeQueryExecutionRepo())

	tests := []struct {
		name   string
		method string
		path   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "get query", method: http.MethodGet, path: "/api/v1/queries/" + missingID, call: handler.GetQuery},
		{name: "delete query", method: http.MethodDelete, path: "/api/v1/queries/" + missingID, call: handler.DeleteQuery},
		{name: "list query results", method: http.MethodGet, path: "/api/v1/queries/" + missingID + "/results", call: handler.ListQueryResults},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			setUser(req, "another-user")
			req.SetPathValue("id", missingID)
			rec := httptest.NewRecorder()

			tt.call(rec, req)

			assertStatus(t, rec, http.StatusForbidden)
			assertBodyContains(t, rec, "saved query belongs to another user")
		})
	}
}

func TestResultEndpointsMissingResultReturnNotFound(t *testing.T) {
	handler := newTestHandler()

	tests := []struct {
		name   string
		method string
		path   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "delete result", method: http.MethodDelete, path: "/api/v1/results/" + missingID, call: handler.DeleteResult},
		{name: "get result data", method: http.MethodGet, path: "/api/v1/results/" + missingID + "/data", call: handler.GetResultData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			setUser(req, "test-user")
			req.SetPathValue("id", missingID)
			rec := httptest.NewRecorder()

			tt.call(rec, req)

			assertStatus(t, rec, http.StatusNotFound)
			assertBodyContains(t, rec, "query result with id "+missingID+" was not found")
		})
	}
}

func TestResultEndpointsRejectOtherUser(t *testing.T) {
	queryRepo := &fakeSavedQueryRepo{
		query: &model.SavedQuery{
			ID:         missingID,
			UserID:     "owner-user",
			Name:       "Test query",
			Visibility: model.VisibilityPrivate,
			QueryType:  "FSG",
			Params:     `{}`,
		},
	}
	resultRepo := &fakeQueryResultRepo{
		result: &model.QueryResult{
			ID:      "3a4094b2-07d2-4ee2-820b-f2dfda57e0e7",
			QueryID: missingID,
		},
	}
	handler := newTestHandlerWithRepos(queryRepo, resultRepo, newFakeQueryExecutionRepo())

	tests := []struct {
		name   string
		method string
		path   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "delete result", method: http.MethodDelete, path: "/api/v1/results/" + resultRepo.result.ID, call: handler.DeleteResult},
		{name: "get result data", method: http.MethodGet, path: "/api/v1/results/" + resultRepo.result.ID + "/data", call: handler.GetResultData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			setUser(req, "another-user")
			req.SetPathValue("id", resultRepo.result.ID)
			rec := httptest.NewRecorder()

			tt.call(rec, req)

			assertStatus(t, rec, http.StatusForbidden)
			assertBodyContains(t, rec, "query result belongs to another user")
		})
	}
}

func TestDictionaryEndpoints(t *testing.T) {
	handler := newDictionaryTestHandler()

	upsertReq := httptest.NewRequest(http.MethodPost, "/api/v1/dictionaries", strings.NewReader(`{
		"business": "FB",
		"dictionary_code": "BUDGETS",
		"code": "99010001",
		"short_name": "Federal budget",
		"full_name": "Federal budget for revenue accounting",
		"analytical_attribute_code": "budgetCode"
	}`))
	upsertRec := httptest.NewRecorder()
	handler.UpsertDictionaryItem(upsertRec, upsertReq)

	assertStatus(t, upsertRec, http.StatusOK)
	assertBodyContains(t, upsertRec, `"analytical_attribute_code":"budgetCode"`)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/dictionaries?business=FB&dictionary_code=BUDGETS&q=revenue&analytical_attribute_code=budgetCode", nil)
	listRec := httptest.NewRecorder()
	handler.ListDictionaryItems(listRec, listReq)

	assertStatus(t, listRec, http.StatusOK)
	assertBodyContains(t, listRec, `"code":"99010001"`)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/dictionaries/item?business=FB&dictionary_code=BUDGETS&item_code=99010001", nil)
	getRec := httptest.NewRecorder()
	handler.GetDictionaryItem(getRec, getReq)

	assertStatus(t, getRec, http.StatusOK)
	assertBodyContains(t, getRec, `"short_name":"Federal budget"`)
	assertBodyContains(t, getRec, `"full_name":"Federal budget for revenue accounting"`)
}

func TestListAnalyticalAttributes(t *testing.T) {
	attrRepo := &fakeAnalyticalAttributeRepo{
		attrs: []model.AnalyticalAttribute{
			{Code: "budgetCode", Name: "Бюджет", Businesses: []string{"FB"}},
			{Code: "kbk", Name: "КБК", Businesses: []string{"FB"}},
		},
	}
	aggregator := service.NewAggregatorService(&fakeSVAPClient{}, attrRepo, &fakeSavedQueryRepo{}, &fakeQueryResultRepo{}, newFakeDictionaryRepo(), nil, newFakeQueryExecutionRepo())
	handler := NewHandler(aggregator, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytical-attributes?business=FB", nil)
	rec := httptest.NewRecorder()

	handler.ListAnalyticalAttributes(rec, req)

	assertStatus(t, rec, http.StatusOK)
	assertBodyContains(t, rec, `"code":"budgetCode"`)
	assertBodyContains(t, rec, `"name":"Бюджет"`)
}

func newTestHandler() *Handler {
	return newTestHandlerWithRepos(&fakeSavedQueryRepo{}, &fakeQueryResultRepo{}, newFakeQueryExecutionRepo())
}

func newDictionaryTestHandler() *Handler {
	aggregator := service.NewAggregatorService(&fakeSVAPClient{}, nil, &fakeSavedQueryRepo{}, &fakeQueryResultRepo{}, newFakeDictionaryRepo(), nil, newFakeQueryExecutionRepo())
	return NewHandler(aggregator, nil)
}

func newTestHandlerWithRepos(queryRepo *fakeSavedQueryRepo, resultRepo *fakeQueryResultRepo, executionRepo *fakeQueryExecutionRepo) *Handler {
	return newTestHandlerWithClient(&fakeSVAPClient{}, queryRepo, resultRepo, executionRepo)
}

func newTestHandlerWithClient(client *fakeSVAPClient, queryRepo *fakeSavedQueryRepo, resultRepo *fakeQueryResultRepo, executionRepo *fakeQueryExecutionRepo) *Handler {
	aggregator := service.NewAggregatorService(client, nil, queryRepo, resultRepo, nil, nil, executionRepo)
	return NewHandler(aggregator, nil)
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func assertBodyContains(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), want)
	}
}

func setUser(req *http.Request, userID string) {
	req.Header.Set("X-User-ID", userID)
}

type fakeSavedQueryRepo struct {
	query *model.SavedQuery
}

func (r *fakeSavedQueryRepo) Save(ctx context.Context, q model.SavedQuery) (string, error) {
	return "", errors.New("unexpected Save call")
}

func (r *fakeSavedQueryRepo) GetByID(ctx context.Context, id string) (model.SavedQuery, error) {
	if r.query != nil && r.query.ID == id {
		return *r.query, nil
	}
	return model.SavedQuery{}, repository.ErrNotFound
}

func (r *fakeSavedQueryRepo) GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error) {
	return nil, errors.New("unexpected GetByUserID call")
}

func (r *fakeSavedQueryRepo) GetAll(ctx context.Context) ([]model.SavedQuery, error) {
	return nil, errors.New("unexpected GetAll call")
}

func (r *fakeSavedQueryRepo) Update(ctx context.Context, q model.SavedQuery) error {
	return errors.New("unexpected Update call")
}

func (r *fakeSavedQueryRepo) Delete(ctx context.Context, id string) error {
	return errors.New("unexpected Delete call")
}

type fakeQueryResultRepo struct {
	saveCh chan model.QueryResult
	result *model.QueryResult
}

func (r *fakeQueryResultRepo) Save(ctx context.Context, res model.QueryResult, rows []model.QueryResultRow, values []model.QueryResultValue) (string, error) {
	if r.saveCh == nil {
		return "3a4094b2-07d2-4ee2-820b-f2dfda57e0e7", nil
	}
	r.saveCh <- res
	return "3a4094b2-07d2-4ee2-820b-f2dfda57e0e7", nil
}

func (r *fakeQueryResultRepo) GetByQueryID(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetByQueryID call")
}

func (r *fakeQueryResultRepo) GetByUserID(ctx context.Context, userID string) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetByUserID call")
}

func (r *fakeQueryResultRepo) GetByID(ctx context.Context, id string) (model.QueryResult, error) {
	if r.result != nil && r.result.ID == id {
		return *r.result, nil
	}
	return model.QueryResult{}, repository.ErrNotFound
}

func (r *fakeQueryResultRepo) GetAll(ctx context.Context) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetAll call")
}

func (r *fakeQueryResultRepo) GetRows(ctx context.Context, resultID string) ([]model.QueryResultRow, error) {
	return nil, errors.New("unexpected GetRows call")
}

func (r *fakeQueryResultRepo) GetValuesByRowID(ctx context.Context, rowID string) ([]model.QueryResultValue, error) {
	return nil, errors.New("unexpected GetValuesByRowID call")
}

func (r *fakeQueryResultRepo) GetFullResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error) {
	return nil, errors.New("unexpected GetFullResultData call")
}

func (r *fakeQueryResultRepo) Delete(ctx context.Context, id string) error {
	return errors.New("unexpected Delete call")
}

type fakeSVAPClient struct {
	executeErr error
}

func (c *fakeSVAPClient) GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	return nil, errors.New("unexpected GetAnalyticalAttributes call")
}

func (c *fakeSVAPClient) ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document any) (any, error) {
	if c.executeErr != nil {
		return nil, c.executeErr
	}
	return map[string]any{"document": map[string]any{"RespData": []any{}}}, nil
}

type fakeAnalyticalAttributeRepo struct {
	attrs []model.AnalyticalAttribute
}

func (r *fakeAnalyticalAttributeRepo) SaveAll(ctx context.Context, attrs []model.AnalyticalAttribute) error {
	r.attrs = attrs
	return nil
}

func (r *fakeAnalyticalAttributeRepo) GetAll(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	return r.attrs, nil
}

func (r *fakeAnalyticalAttributeRepo) GetByBusiness(ctx context.Context, business string) ([]model.AnalyticalAttribute, error) {
	var result []model.AnalyticalAttribute
	for _, attr := range r.attrs {
		for _, attrBusiness := range attr.Businesses {
			if attrBusiness == business {
				result = append(result, attr)
				break
			}
		}
	}
	return result, nil
}

type fakeDictionaryRepo struct {
	items map[string]model.DictionaryItem
}

func newFakeDictionaryRepo() *fakeDictionaryRepo {
	return &fakeDictionaryRepo{items: make(map[string]model.DictionaryItem)}
}

func (r *fakeDictionaryRepo) Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error) {
	item, err := r.GetItem(ctx, business, dictionaryCode, key)
	if err != nil {
		return "", err
	}
	return item.ShortName, nil
}

func (r *fakeDictionaryRepo) Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error {
	_, err := r.UpsertItem(ctx, model.DictionaryItem{
		Business:       business,
		DictionaryCode: dictionaryCode,
		Code:           key,
		ShortName:      value,
		FullName:       value,
	})
	return err
}

func (r *fakeDictionaryRepo) GetItem(ctx context.Context, business string, dictionaryCode string, itemCode string) (model.DictionaryItem, error) {
	item, ok := r.items[dictionaryKey(business, dictionaryCode, itemCode)]
	if !ok {
		return model.DictionaryItem{}, repository.ErrNotFound
	}
	return item, nil
}

func (r *fakeDictionaryRepo) UpsertItem(ctx context.Context, item model.DictionaryItem) (model.DictionaryItem, error) {
	item.LastUpdated = time.Now()
	r.items[dictionaryKey(item.Business, item.DictionaryCode, item.Code)] = item
	return item, nil
}

func (r *fakeDictionaryRepo) DeleteItem(ctx context.Context, business string, dictionaryCode string, itemCode string) error {
	key := dictionaryKey(business, dictionaryCode, itemCode)
	if _, ok := r.items[key]; !ok {
		return repository.ErrNotFound
	}
	delete(r.items, key)
	return nil
}

func (r *fakeDictionaryRepo) Search(ctx context.Context, filter model.DictionaryFilter) ([]model.DictionaryItem, error) {
	var result []model.DictionaryItem
	for _, item := range r.items {
		if filter.Business != "" && item.Business != filter.Business {
			continue
		}
		if filter.DictionaryCode != "" && item.DictionaryCode != filter.DictionaryCode {
			continue
		}
		if filter.AnalyticalAttributeCode != "" && item.AnalyticalAttributeCode != filter.AnalyticalAttributeCode {
			continue
		}
		if filter.Query != "" && !strings.Contains(item.ShortName, filter.Query) && !strings.Contains(item.FullName, filter.Query) {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func dictionaryKey(business string, dictionaryCode string, itemCode string) string {
	return business + "/" + dictionaryCode + "/" + itemCode
}

type fakeQueryExecutionRepo struct {
	executions  map[string]model.QueryExecution
	nextID      string
	succeededCh chan struct{}
}

func newFakeQueryExecutionRepo() *fakeQueryExecutionRepo {
	return &fakeQueryExecutionRepo{
		executions: make(map[string]model.QueryExecution),
		nextID:     "8e57023b-b6af-4d89-89b8-f5bb7a79f5d5",
	}
}

func (r *fakeQueryExecutionRepo) Create(ctx context.Context, execution model.QueryExecution) (model.QueryExecution, error) {
	execution.ID = r.nextID
	r.executions[execution.ID] = execution
	return execution, nil
}

func (r *fakeQueryExecutionRepo) MarkRunning(ctx context.Context, id string, startedAt time.Time) error {
	execution, ok := r.executions[id]
	if !ok {
		return repository.ErrNotFound
	}
	execution.Status = model.QueryExecutionStatusRunning
	execution.StartedAt = &startedAt
	r.executions[id] = execution
	return nil
}

func (r *fakeQueryExecutionRepo) MarkSucceeded(ctx context.Context, id string, resultID string, finishedAt time.Time) error {
	execution, ok := r.executions[id]
	if !ok {
		return repository.ErrNotFound
	}
	execution.Status = model.QueryExecutionStatusSucceeded
	execution.ResultID = resultID
	execution.FinishedAt = &finishedAt
	r.executions[id] = execution
	if r.succeededCh != nil {
		close(r.succeededCh)
		r.succeededCh = nil
	}
	return nil
}

func (r *fakeQueryExecutionRepo) MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error {
	execution, ok := r.executions[id]
	if !ok {
		return repository.ErrNotFound
	}
	execution.Status = model.QueryExecutionStatusFailed
	execution.ErrorMessage = message
	execution.FinishedAt = &finishedAt
	r.executions[id] = execution
	return nil
}

func (r *fakeQueryExecutionRepo) GetByID(ctx context.Context, id string) (model.QueryExecution, error) {
	execution, ok := r.executions[id]
	if !ok {
		return model.QueryExecution{}, repository.ErrNotFound
	}
	return execution, nil
}

func (r *fakeQueryExecutionRepo) GetByUserID(ctx context.Context, userID string) ([]model.QueryExecution, error) {
	var executions []model.QueryExecution
	for _, execution := range r.executions {
		if execution.UserID == userID {
			executions = append(executions, execution)
		}
	}
	return executions, nil
}
