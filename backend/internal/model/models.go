package model

import (
	"fmt"
	"strings"
	"time"
)

// AnalyticalAttribute представляет аналитический признак (АП) из СВАП
type AnalyticalAttribute struct {
	Code            string    `json:"code"`             // Код ан признака СВАП (напр. BUD_CODE)
	Name            string    `json:"name"`             // Наименование ан признака
	Businesses      []string  `json:"businesses"`       // Перечень СВАПОВ (бизнесов), напр. ["ФБ", "ПУД"]
	InAccount       bool      `json:"in_account"`       // Тип признака (в составе счета / не в составе)
	UseInBalance    bool      `json:"use_in_balance"`   // Признак участия в расчете балансов
	ValidationType  string    `json:"validation_type"`  // Тип валидации (Service/RegExpression/List)
	ValidationValue string    `json:"validation_value"` // Значение для валидации (адрес сервиса/регэксп/JSON списка)
	LastUpdated     time.Time `json:"last_updated"`
}

// SVAPTime кастомный тип времени для соответствия формату СВАП (yyyy-MM-dd HH:mm:ss)
type SVAPTime time.Time

func (t SVAPTime) MarshalJSON() ([]byte, error) {
	formatted := fmt.Sprintf("\"%s\"", time.Time(t).Format("2006-01-02 15:04:05"))
	return []byte(formatted), nil
}

func (t *SVAPTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), "\"")
	if s == "null" || s == "" {
		return nil
	}
	parsed, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		// Попробуем стандартный RFC3339 на всякий случай
		parsed, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
	}
	*t = SVAPTime(parsed)
	return nil
}

// SVAPHeader общий заголовок запроса к СВАП
type SVAPHeader struct {
	Source       string   `json:"Source" xml:"Subject"`
	ReqDateTime  SVAPTime `json:"ReqDateTime" xml:"SettlementDate"`
	Version      string   `json:"Version"`
	ReqGuid      string   `json:"ReqGuid"`
	DocGuid      string   `json:"DocGuid"`
	RequestType  string   `json:"RequestType"`
	Reprocessing string   `json:"Reprocessing,omitempty" xml:"Reprocessing"`
	Offset       int      `json:"Offset,omitempty" xml:"Offset,omitempty"`
	Limit        int      `json:"Limit,omitempty" xml:"Limit,omitempty"`
}

// SavedQuery представляет сохраненный запрос пользователя
type SavedQuery struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Visibility  string    `json:"visibility"` // private, organization, public
	QueryType   string    `json:"query_type"` // OPLIST, FSG, TURN, ReportGK, COR, PA, CONS
	Params      string    `json:"params"`     // JSON-строка параметров
	CreatedAt   time.Time `json:"created_at"`
}

const (
	VisibilityPrivate      = "private"
	VisibilityOrganization = "organization"
	VisibilityPublic       = "public"
)

// QueryResult представляет метаданные результата выполнения запроса
type QueryResult struct {
	ID          string     `json:"id"`
	QueryID     string     `json:"query_id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Visibility  string     `json:"visibility"`
	Meta        any        `json:"meta"` // Метаинформация для АИ
	FetchedAt   time.Time  `json:"fetched_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

const (
	QueryExecutionModeSync  = "sync"
	QueryExecutionModeAsync = "async"

	QueryExecutionStatusQueued    = "queued"
	QueryExecutionStatusRunning   = "running"
	QueryExecutionStatusSucceeded = "succeeded"
	QueryExecutionStatusFailed    = "failed"
)

// QueryExecution отражает статус задачи выполнения сохраненного запроса.
type QueryExecution struct {
	ID           string     `json:"id"`
	QueryID      string     `json:"query_id"`
	UserID       string     `json:"user_id"`
	Status       string     `json:"status"`
	Mode         string     `json:"mode"`
	StartRepDate string     `json:"start_rep_date,omitempty"`
	EndRepDate   string     `json:"end_rep_date,omitempty"`
	Offset       int        `json:"offset"`
	Limit        int        `json:"limit"`
	ResultID     string     `json:"result_id,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// QueryResultRow представляет одну строку данных (сущность)
type QueryResultRow struct {
	ID        string    `json:"id"`
	ResultID  string    `json:"result_id"`
	CreatedAt time.Time `json:"created_at"`
}

// QueryResultValue представляет конкретное значение атрибута (измерения или показателя) в строке
type QueryResultValue struct {
	ID             int      `json:"id"`
	RowID          string   `json:"row_id"`
	AttributeCode  string   `json:"attribute_code"`
	AttributeValue string   `json:"attribute_value"`
	NumericValue   *float64 `json:"numeric_value,omitempty"` // nil если значение не числовое
}

// DictionaryItem элемент справочника из ФП
type DictionaryItem struct {
	Business                string    `json:"business"`
	DictionaryCode          string    `json:"dictionary_code"`
	Code                    string    `json:"code"`
	ShortName               string    `json:"short_name"`
	FullName                string    `json:"full_name"`
	AnalyticalAttributeCode string    `json:"analytical_attribute_code,omitempty"`
	LastUpdated             time.Time `json:"last_updated"`
}

type DictionaryFilter struct {
	Business                string
	DictionaryCode          string
	Query                   string
	AnalyticalAttributeCode string
}
