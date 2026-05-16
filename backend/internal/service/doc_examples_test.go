package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"svap-query-service/backend/internal/model"
	"svap-query-service/backend/internal/repository"
)

func TestDocumentationExamplesExecuteAndParse(t *testing.T) {
	examples := []struct {
		queryType      string
		params         map[string]any
		requiredParams []string
		expectedValues []string
	}{
		{
			queryType: "FSG",
			params: map[string]any{
				"Mode": 0, "BeginBalance": 1, "DateMode": 0, "Book": "FTOperFed",
				"StartRepDate": "2026-01-01", "EndRepDate": "2026-03-31", "RecType": 1, "NullStr": 0, "ReturnIfEmpty": 1,
				"FilterParams": []map[string]any{{"param": "budgetCode", "operation": "=", "value": "99010001"}},
				"VisualParams": []string{"budgetCode", "kbk", "account"},
			},
			requiredParams: []string{"Mode", "BeginBalance", "DateMode", "Book", "StartRepDate", "EndRepDate", "RecType", "NullStr", "ReturnIfEmpty", "FilterParams", "VisualParams"},
			expectedValues: []string{"budgetCode", "kbk", "Turnover_dr", "End_balance_cr"},
		},
		{
			queryType: "TURN",
			params: map[string]any{
				"Book": "FTOperFed", "StartRepDate": "2026-01-01", "EndRepDate": "2026-03-31", "RecType": 1, "NullStr": 0, "ReturnIfEmpty": 1,
				"FilterParams": []map[string]any{{"param": "account", "operation": "=", "value": "20202"}},
				"VisualParams": []string{"budgetCode", "account", "documentId"},
			},
			requiredParams: []string{"Book", "StartRepDate", "EndRepDate", "RecType", "NullStr", "ReturnIfEmpty", "FilterParams", "VisualParams"},
			expectedValues: []string{"budgetCode", "account", "Turnover_dr", "Turnover_cr"},
		},
		{
			queryType: "COR",
			params: map[string]any{
				"RptSetCode": 10, "GroupByDocs": 1, "GroupByCorrespontents": 0, "Book": "FTOperFed",
				"StartRepDate": "2026-01-01", "EndRepDate": "2026-03-31", "RecType": 1, "NullStr": 0, "ReturnIfEmpty": 1,
				"TypeDoc":      []string{"VLS_REPORT_DO"},
				"FilterParams": []map[string]any{{"param": "account", "Site": "-1", "operation": "=", "value": "20202"}},
				"VisualParams": []map[string]any{{"param": "budgetCode", "Site": "-1"}, {"param": "account", "Site": "+1"}},
			},
			requiredParams: []string{"RptSetCode", "GroupByDocs", "GroupByCorrespontents", "Book", "StartRepDate", "EndRepDate", "RecType", "NullStr", "ReturnIfEmpty", "TypeDoc", "FilterParams", "VisualParams"},
			expectedValues: []string{"Dt.budgetCode", "Ct.account", "Amnt_val_cur", "Amnt_oper_cur"},
		},
		{
			queryType: "PA",
			params: map[string]any{
				"ReportType": "PA_SVR", "Indicators": "136,137", "Documents": 0, "DateMode": 0, "CurrencyCodeShow": 1,
				"StartRepDate": "2026-01-01", "EndRepDate": "2026-03-31", "RecType": 1, "NullStr": 0, "ReturnIfEmpty": 1,
				"FilterParams": []map[string]any{{"param": "pa", "operation": "=", "value": "036001010"}},
				"VisualParams": []string{"pa", "budgetCode"},
			},
			requiredParams: []string{"ReportType", "Indicators", "Documents", "DateMode", "CurrencyCodeShow", "StartRepDate", "EndRepDate", "RecType", "NullStr", "ReturnIfEmpty", "FilterParams", "VisualParams"},
			expectedValues: []string{"pa", "budgetCode", "DATA_136", "DATA_137"},
		},
		{
			queryType: "CONS",
			params: map[string]any{
				"ReportType": "PA_SVR", "Documents": 0, "DateMode": 0, "CurrencyCodeShow": 1,
				"StartRepDate": "2026-01-01", "EndRepDate": "2026-03-31", "RecType": 1, "NullStr": 0, "ReturnIfEmpty": 1,
				"Groups": []map[string]any{{
					"indicators":   "136,137",
					"FilterParams": []map[string]any{{"param": "budgetCode", "operation": "=", "value": "99010001"}},
				}},
				"VisualParams": []string{"budgetCode", "kbk"},
			},
			requiredParams: []string{"ReportType", "Documents", "DateMode", "CurrencyCodeShow", "StartRepDate", "EndRepDate", "RecType", "NullStr", "ReturnIfEmpty", "Groups", "VisualParams"},
			expectedValues: []string{"budgetCode", "kbk", "DATA_136", "DATA_137"},
		},
	}

	for _, example := range examples {
		t.Run(example.queryType, func(t *testing.T) {
			params, err := json.Marshal(example.params)
			if err != nil {
				t.Fatalf("marshal params: %v", err)
			}
			queryID := "query-" + example.queryType
			client := &docExampleSVAPClient{}
			resultRepo := &docExampleResultRepo{}
			service := NewAggregatorService(
				client,
				nil,
				&docExampleQueryRepo{query: model.SavedQuery{
					ID:         queryID,
					UserID:     "test-user",
					Name:       example.queryType + " doc example",
					Visibility: model.VisibilityPrivate,
					QueryType:  example.queryType,
					Params:     string(params),
				}},
				resultRepo,
				nil,
				nil,
			)

			if _, err := service.ExecuteAndSaveQuery(context.Background(), queryID, 0, 100, "", "", "", "", "", nil, ""); err != nil {
				t.Fatalf("ExecuteAndSaveQuery() error = %v", err)
			}

			if client.queryType != example.queryType {
				t.Fatalf("queryType = %q, want %q", client.queryType, example.queryType)
			}
			if client.header.RequestType != example.queryType {
				t.Fatalf("RequestType = %q, want %q", client.header.RequestType, example.queryType)
			}
			if client.header.Offset != 0 || client.header.Limit != 0 {
				t.Fatalf("SVAP header has pagination Offset=%d Limit=%d; datamart report endpoints reject these fields", client.header.Offset, client.header.Limit)
			}
			document, ok := client.document.(map[string]any)
			if !ok {
				t.Fatalf("document type = %T, want map[string]any", client.document)
			}
			for _, key := range example.requiredParams {
				if _, ok := document[key]; !ok {
					t.Fatalf("document missing %q: %#v", key, document)
				}
			}

			valueCodes := make(map[string]bool, len(resultRepo.values))
			for _, value := range resultRepo.values {
				valueCodes[value.AttributeCode] = true
			}
			for _, code := range example.expectedValues {
				if !valueCodes[code] {
					t.Fatalf("saved values missing %q; got %#v", code, valueCodes)
				}
			}
		})
	}
}

type docExampleSVAPClient struct {
	queryType string
	header    model.SVAPHeader
	document  any
}

func (c *docExampleSVAPClient) GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	return nil, errors.New("unexpected GetAnalyticalAttributes call")
}

func (c *docExampleSVAPClient) ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document any) (any, error) {
	c.queryType = queryType
	c.header = header
	c.document = document

	switch queryType {
	case "FSG":
		return respData(map[string]any{
			"VisualParams": []any{
				map[string]any{"param": "budgetCode", "value": "99010001"},
				map[string]any{"param": "kbk", "value": "1770101010101"},
			},
			"Turnover_dr":    125.50,
			"End_balance_cr": 20.00,
		}), nil
	case "TURN":
		return respData(map[string]any{
			"VisualParams": []any{
				map[string]any{"param": "budgetCode", "value": "99010001"},
				map[string]any{"param": "account", "value": "20202"},
			},
			"Turnover_dr": 100.00,
			"Turnover_cr": 75.00,
		}), nil
	case "COR":
		return respData(map[string]any{
			"VisualParams_Dt": []any{map[string]any{"param": "budgetCode", "value": "99010001"}},
			"VisualParams_Cr": []any{map[string]any{"param": "account", "value": "30303"}},
			"Amnt_val_cur":    300.00,
			"Amnt_oper_cur":   300.00,
		}), nil
	case "PA", "CONS":
		return respData(map[string]any{
			"VisualParams": []any{
				map[string]any{"param": "pa", "value": "036001010"},
				map[string]any{"param": "budgetCode", "value": "99010001"},
				map[string]any{"param": "kbk", "value": "1770101010101"},
			},
			"Indicators": []any{
				map[string]any{"data": "DATA_136", "value": 10.00},
				map[string]any{"data": "DATA_137", "value": 5.00},
			},
		}), nil
	default:
		return nil, fmt.Errorf("unexpected query type %s", queryType)
	}
}

func respData(row map[string]any) map[string]any {
	return map[string]any{"document": map[string]any{"RespData": []any{row}}}
}

type docExampleQueryRepo struct {
	query model.SavedQuery
}

func (r *docExampleQueryRepo) Save(ctx context.Context, q model.SavedQuery) (string, error) {
	return "", errors.New("unexpected Save call")
}

func (r *docExampleQueryRepo) GetByID(ctx context.Context, id string) (model.SavedQuery, error) {
	if r.query.ID == id {
		return r.query, nil
	}
	return model.SavedQuery{}, repository.ErrNotFound
}

func (r *docExampleQueryRepo) GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error) {
	return nil, errors.New("unexpected GetByUserID call")
}

func (r *docExampleQueryRepo) GetAll(ctx context.Context) ([]model.SavedQuery, error) {
	return nil, errors.New("unexpected GetAll call")
}

func (r *docExampleQueryRepo) Update(ctx context.Context, q model.SavedQuery) error {
	return errors.New("unexpected Update call")
}

func (r *docExampleQueryRepo) Delete(ctx context.Context, id string) error {
	return errors.New("unexpected Delete call")
}

type docExampleResultRepo struct {
	values []model.QueryResultValue
}

func (r *docExampleResultRepo) Save(ctx context.Context, res model.QueryResult, rows []model.QueryResultRow, values []model.QueryResultValue) (string, error) {
	r.values = values
	return "result-id", nil
}

func (r *docExampleResultRepo) GetByQueryID(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetByQueryID call")
}

func (r *docExampleResultRepo) GetByUserID(ctx context.Context, userID string) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetByUserID call")
}

func (r *docExampleResultRepo) GetByID(ctx context.Context, id string) (model.QueryResult, error) {
	return model.QueryResult{}, errors.New("unexpected GetByID call")
}

func (r *docExampleResultRepo) GetAll(ctx context.Context) ([]model.QueryResult, error) {
	return nil, errors.New("unexpected GetAll call")
}

func (r *docExampleResultRepo) GetRows(ctx context.Context, resultID string) ([]model.QueryResultRow, error) {
	return nil, errors.New("unexpected GetRows call")
}

func (r *docExampleResultRepo) GetValuesByRowID(ctx context.Context, rowID string) ([]model.QueryResultValue, error) {
	return nil, errors.New("unexpected GetValuesByRowID call")
}

func (r *docExampleResultRepo) GetFullResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error) {
	return nil, errors.New("unexpected GetFullResultData call")
}

func (r *docExampleResultRepo) Delete(ctx context.Context, id string) error {
	return errors.New("unexpected Delete call")
}
