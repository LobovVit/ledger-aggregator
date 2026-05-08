package model

import "time"

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

// SVAPHeader общий заголовок запроса к СВАП
type SVAPHeader struct {
	Subject        string    `xml:"Subject"`        // Инициатор события учета
	SettlementDate time.Time `xml:"SettlementDate"` // Дата отражения
	Reprocessing   string    `xml:"Reprocessing"`   // Признак переобработки
	Offset         int       `xml:"Offset,omitempty"`
	Limit          int       `xml:"Limit,omitempty"`
}

// SavedQuery представляет сохраненный запрос пользователя
type SavedQuery struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	System    string    `json:"system"`     // Напр. "ГК", "ЛС"
	QueryType string    `json:"query_type"` // OPLIST, FSG, TURN, ReportGK, COR, PA, CONS
	Params    string    `json:"params"`     // JSON-строка параметров
	CreatedAt time.Time `json:"created_at"`
}

// QueryResult представляет метаданные результата выполнения запроса
type QueryResult struct {
	ID        string    `json:"id"`
	QueryID   string    `json:"query_id"`
	Meta      any       `json:"meta"` // Метаинформация для АИ
	FetchedAt time.Time `json:"fetched_at"`
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
	Code string `json:"code"`
	Name string `json:"name"`
}
