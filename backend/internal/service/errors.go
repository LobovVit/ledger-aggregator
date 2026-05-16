package service

import "errors"

var (
	ErrSavedQueryNotFound     = errors.New("saved query not found")
	ErrQueryResultNotFound    = errors.New("query result not found")
	ErrQueryExecutionNotFound = errors.New("query execution not found")
	ErrDictionaryItemNotFound = errors.New("dictionary item not found")
	ErrForbidden              = errors.New("forbidden")
)
