package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"svap-query-service/backend/internal/config"
	"svap-query-service/backend/internal/model"
	"svap-query-service/backend/internal/service"
	"svap-query-service/backend/internal/version"

	"github.com/google/uuid"
)

const maxPageLimit = 1000

// ErrorResponse documents the error shape in Swagger. Runtime handlers still
// use http.Error so existing clients keep receiving plain-text error bodies.
type ErrorResponse struct {
	Message string `json:"message" example:"saved query with id ... was not found"`
}

type Handler struct {
	aggregator *service.AggregatorService
	config     *config.ConfigService
}

func NewHandler(aggregator *service.AggregatorService, config *config.ConfigService) *Handler {
	return &Handler{
		aggregator: aggregator,
		config:     config,
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/query/execute", h.ExecuteQuery)
	mux.HandleFunc("POST /api/v1/queries", h.CreateQuery)
	mux.HandleFunc("GET /api/v1/queries", h.ListQueries)
	mux.HandleFunc("GET /api/v1/queries/{id}", h.GetQuery)
	mux.HandleFunc("DELETE /api/v1/queries/{id}", h.DeleteQuery)
	mux.HandleFunc("GET /api/v1/queries/{id}/results", h.ListQueryResults)
	mux.HandleFunc("GET /api/v1/query/executions", h.ListQueryExecutions)
	mux.HandleFunc("GET /api/v1/query/executions/{id}", h.GetQueryExecution)
	mux.HandleFunc("GET /api/v1/results", h.ListResults)
	mux.HandleFunc("DELETE /api/v1/results/{id}", h.DeleteResult)
	mux.HandleFunc("GET /api/v1/results/{id}/data", h.GetResultData)
	mux.HandleFunc("GET /api/v1/dictionaries", h.ListDictionaryItems)
	mux.HandleFunc("POST /api/v1/dictionaries", h.UpsertDictionaryItem)
	mux.HandleFunc("GET /api/v1/dictionaries/item", h.GetDictionaryItem)
	mux.HandleFunc("DELETE /api/v1/dictionaries/item", h.DeleteDictionaryItem)
	mux.HandleFunc("GET /api/v1/analytical-attributes", h.ListAnalyticalAttributes)

	// Info routes
	mux.HandleFunc("GET /api/v1/info", h.GetAppInfo)

	// Config routes
	mux.HandleFunc("GET /api/v1/config", h.GetConfig)
	mux.HandleFunc("GET /api/v1/config/groups", h.ListConfigGroups)
	mux.HandleFunc("GET /api/v1/config/groups/{name}", h.GetConfigGroup)
	mux.HandleFunc("PUT /api/v1/config", h.UpdateConfig)
	mux.HandleFunc("POST /api/v1/config/apply", h.ApplyConfig)
}

// ListAnalyticalAttributes godoc
// @Summary RU: Получить аналитические признаки / EN: List analytical attributes
// @Description RU: Возвращает справочник аналитических признаков, опционально с фильтром по бизнесу. EN: Returns analytical attributes, optionally filtered by business.
// @Tags dictionaries
// @Produce json
// @Param business query string false "RU: Код бизнеса / EN: Business code"
// @Success 200 {array} model.AnalyticalAttribute "RU: Список аналитических признаков / EN: Analytical attribute list"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /analytical-attributes [get]
func (h *Handler) ListAnalyticalAttributes(w http.ResponseWriter, r *http.Request) {
	attrs, err := h.aggregator.GetAnalyticalAttributes(r.Context(), r.URL.Query().Get("business"))
	if err != nil {
		http.Error(w, "failed to list analytical attributes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attrs)
}

// CreateQuery godoc
// @Summary RU: Создать сохраненный запрос / EN: Create a saved query
// @Description RU: Регистрирует новый шаблон запроса с параметрами фильтрации. Пользователь определяется по заголовку X-User-ID. EN: Registers a new query template with filter parameters. The user is resolved from the X-User-ID header.
// @Tags queries
// @Accept json
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param query body model.SavedQuery true "RU: Описание запроса / EN: Query definition"
// @Success 201 {object} model.SavedQuery "RU: Запрос создан / EN: Query created"
// @Failure 400 {string} string "RU: Некорректное тело запроса или видимость / EN: Invalid request body or visibility"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: user_id не совпадает с X-User-ID / EN: user_id does not match X-User-ID"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries [post]
func (h *Handler) CreateQuery(w http.ResponseWriter, r *http.Request) {
	var q model.SavedQuery
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	if q.UserID != "" && q.UserID != requestUserID {
		http.Error(w, "user_id does not match authenticated user", http.StatusForbidden)
		return
	}
	q.UserID = requestUserID

	if err := validateSavedQuery(q); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	saved, err := h.aggregator.CreateSavedQuery(r.Context(), q)
	if err != nil {
		http.Error(w, "failed to create query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(saved)
}

// ListQueries godoc
// @Summary RU: Получить сохраненные запросы / EN: List saved queries
// @Description RU: Возвращает список сохраненных запросов пользователя из заголовка X-User-ID. EN: Returns saved queries for the user from the X-User-ID header.
// @Tags queries
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Success 200 {array} model.SavedQuery "RU: Список сохраненных запросов / EN: Saved query list"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries [get]
func (h *Handler) ListQueries(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}

	queries, err := h.aggregator.GetSavedQueries(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to list queries: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queries)
}

// DeleteQuery godoc
// @Summary RU: Удалить сохраненный запрос / EN: Delete a saved query
// @Description RU: Удаляет сохраненный запрос и все связанные с ним результаты. EN: Deletes a saved query and all related query results.
// @Tags queries
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 204 "RU: Удалено, тело ответа отсутствует / EN: Deleted, no response body"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Запрос принадлежит другому пользователю / EN: Query belongs to another user"
// @Failure 404 {string} string "RU: Запрос не найден / EN: Query not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries/{id} [delete]
func (h *Handler) DeleteQuery(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.EnsureSavedQueryBelongsToUser(r.Context(), id, userID); err != nil {
		h.writeSavedQueryAccessError(w, id, err)
		return
	}
	if err := h.aggregator.DeleteSavedQuery(r.Context(), id); err != nil {
		if errors.Is(err, service.ErrSavedQueryNotFound) {
			http.Error(w, "saved query with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete query: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetQuery godoc
// @Summary RU: Получить сохраненный запрос / EN: Get a saved query
// @Description RU: Возвращает параметры конкретного сохраненного запроса по UUID. EN: Returns details of a saved query by UUID.
// @Tags queries
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 200 {object} model.SavedQuery "RU: Сохраненный запрос / EN: Saved query"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Запрос принадлежит другому пользователю / EN: Query belongs to another user"
// @Failure 404 {string} string "RU: Запрос не найден / EN: Query not found"
// @Router /queries/{id} [get]
func (h *Handler) GetQuery(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	q, err := h.aggregator.GetSavedQueryByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrSavedQueryNotFound) {
			http.Error(w, "saved query with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to get query: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if q.UserID != userID {
		http.Error(w, "saved query belongs to another user", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

// ListQueryResults godoc
// @Summary RU: Получить результаты запроса / EN: List query results
// @Description RU: Возвращает историю выполнений для указанного сохраненного запроса. EN: Returns execution history for the specified saved query.
// @Tags results
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 200 {array} model.QueryResult "RU: История результатов / EN: Query result history"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Запрос принадлежит другому пользователю / EN: Query belongs to another user"
// @Failure 404 {string} string "RU: Запрос не найден / EN: Query not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries/{id}/results [get]
func (h *Handler) ListQueryResults(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.EnsureSavedQueryBelongsToUser(r.Context(), id, userID); err != nil {
		h.writeSavedQueryAccessError(w, id, err)
		return
	}
	results, err := h.aggregator.GetQueryResults(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrSavedQueryNotFound) {
			http.Error(w, "saved query with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to list results: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// ListResults godoc
// @Summary RU: Получить все результаты пользователя / EN: List user query results
// @Description RU: Возвращает все результаты выполнения запросов пользователя из заголовка X-User-ID. EN: Returns all query execution results for the user from the X-User-ID header.
// @Tags results
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Success 200 {array} model.QueryResult "RU: Список результатов / EN: Query result list"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /results [get]
func (h *Handler) ListResults(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}

	results, err := h.aggregator.GetQueryResultsByUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to list results: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// DeleteResult godoc
// @Summary RU: Удалить результат запроса / EN: Delete a query result
// @Description RU: Удаляет конкретный результат выполнения запроса и связанные строки данных. EN: Deletes a specific query execution result and its stored data rows.
// @Tags results
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID результата / EN: Result UUID"
// @Success 204 "RU: Удалено, тело ответа отсутствует / EN: Deleted, no response body"
// @Failure 400 {string} string "RU: Некорректный UUID результата / EN: Invalid result UUID"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Результат принадлежит другому пользователю / EN: Result belongs to another user"
// @Failure 404 {string} string "RU: Результат не найден / EN: Result not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /results/{id} [delete]
func (h *Handler) DeleteResult(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid result id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.EnsureQueryResultBelongsToUser(r.Context(), id, userID); err != nil {
		h.writeQueryResultAccessError(w, id, err)
		return
	}
	if err := h.aggregator.DeleteQueryResult(r.Context(), id); err != nil {
		if errors.Is(err, service.ErrQueryResultNotFound) {
			http.Error(w, "query result with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to delete result: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetResultData godoc
// @Summary RU: Получить данные результата / EN: Get query result data
// @Description RU: Возвращает строки и значения конкретного результата выполнения с поддержкой пагинации. EN: Returns rows and values for a specific execution result with pagination support.
// @Tags results
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID результата / EN: Result UUID"
// @Param limit query int false "RU: Количество строк, максимум 1000 / EN: Number of rows to return, max 1000"
// @Param offset query int false "RU: Количество строк для пропуска / EN: Number of rows to skip"
// @Success 200 {array} map[string]any "RU: Плоские строки результата / EN: Flat result rows"
// @Failure 400 {string} string "RU: Некорректный UUID, limit или offset / EN: Invalid UUID, limit, or offset"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Результат принадлежит другому пользователю / EN: Result belongs to another user"
// @Failure 404 {string} string "RU: Результат не найден / EN: Result not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /results/{id}/data [get]
func (h *Handler) GetResultData(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	id := r.PathValue("id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid result id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.EnsureQueryResultBelongsToUser(r.Context(), id, userID); err != nil {
		h.writeQueryResultAccessError(w, id, err)
		return
	}

	limit, err := parseOptionalNonNegativeInt(limitStr, 0)
	if err != nil || limit > maxPageLimit {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	offset, err := parseOptionalNonNegativeInt(offsetStr, 0)
	if err != nil {
		http.Error(w, "invalid offset", http.StatusBadRequest)
		return
	}

	data, err := h.aggregator.GetResultData(r.Context(), id, offset, limit)
	if err != nil {
		if errors.Is(err, service.ErrQueryResultNotFound) {
			http.Error(w, "query result with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to get result data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// ListDictionaryItems godoc
// @Summary RU: Найти элементы справочников / EN: Search dictionary items
// @Description RU: Возвращает элементы справочников с фильтрами по ФП, коду справочника, строке поиска по краткому/полному наименованию и связанному аналитическому признаку. EN: Returns dictionary items filtered by business, dictionary code, short/full name search query, and linked analytical attribute.
// @Tags dictionaries
// @Produce json
// @Param business query string false "RU: Код функциональной подсистемы / EN: Business code"
// @Param dictionary_code query string false "RU: Код справочника / EN: Dictionary code"
// @Param q query string false "RU: Поиск по краткому или полному наименованию / EN: Search by short or full name"
// @Param analytical_attribute_code query string false "RU: Код связанного аналитического признака / EN: Linked analytical attribute code"
// @Success 200 {array} model.DictionaryItem "RU: Элементы справочников / EN: Dictionary items"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /dictionaries [get]
func (h *Handler) ListDictionaryItems(w http.ResponseWriter, r *http.Request) {
	filter := model.DictionaryFilter{
		Business:                r.URL.Query().Get("business"),
		DictionaryCode:          r.URL.Query().Get("dictionary_code"),
		Query:                   r.URL.Query().Get("q"),
		AnalyticalAttributeCode: r.URL.Query().Get("analytical_attribute_code"),
	}

	items, err := h.aggregator.SearchDictionaryItems(r.Context(), filter)
	if err != nil {
		http.Error(w, "failed to list dictionary items: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// GetDictionaryItem godoc
// @Summary RU: Получить элемент справочника / EN: Get dictionary item
// @Description RU: Возвращает один элемент справочника по составному ключу. EN: Returns a single dictionary item by composite key.
// @Tags dictionaries
// @Produce json
// @Param business query string true "RU: Код функциональной подсистемы / EN: Business code"
// @Param dictionary_code query string true "RU: Код справочника / EN: Dictionary code"
// @Param item_code query string true "RU: Код элемента / EN: Item code"
// @Success 200 {object} model.DictionaryItem "RU: Элемент справочника / EN: Dictionary item"
// @Failure 400 {string} string "RU: Не передан обязательный параметр / EN: Required parameter is missing"
// @Failure 404 {string} string "RU: Элемент не найден / EN: Item not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /dictionaries/item [get]
func (h *Handler) GetDictionaryItem(w http.ResponseWriter, r *http.Request) {
	business, dictionaryCode, itemCode, ok := dictionaryItemKeyFromRequest(w, r)
	if !ok {
		return
	}

	item, err := h.aggregator.GetDictionaryItem(r.Context(), business, dictionaryCode, itemCode)
	if err != nil {
		h.writeDictionaryItemError(w, business, dictionaryCode, itemCode, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(item)
}

// UpsertDictionaryItem godoc
// @Summary RU: Создать или обновить элемент справочника / EN: Create or update dictionary item
// @Description RU: Сохраняет элемент справочника. Связь с analytical_attribute_code необязательная. EN: Saves a dictionary item. analytical_attribute_code link is optional.
// @Tags dictionaries
// @Accept json
// @Produce json
// @Param item body model.DictionaryItem true "RU: Элемент справочника / EN: Dictionary item"
// @Success 200 {object} model.DictionaryItem "RU: Элемент сохранен / EN: Dictionary item saved"
// @Failure 400 {string} string "RU: Некорректное тело запроса / EN: Invalid request body"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /dictionaries [post]
func (h *Handler) UpsertDictionaryItem(w http.ResponseWriter, r *http.Request) {
	var item model.DictionaryItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateDictionaryItem(item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	saved, err := h.aggregator.UpsertDictionaryItem(r.Context(), item)
	if err != nil {
		http.Error(w, "failed to save dictionary item: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(saved)
}

// DeleteDictionaryItem godoc
// @Summary RU: Удалить элемент справочника / EN: Delete dictionary item
// @Description RU: Удаляет элемент справочника по составному ключу. EN: Deletes a dictionary item by composite key.
// @Tags dictionaries
// @Param business query string true "RU: Код функциональной подсистемы / EN: Business code"
// @Param dictionary_code query string true "RU: Код справочника / EN: Dictionary code"
// @Param item_code query string true "RU: Код элемента / EN: Item code"
// @Success 204 "RU: Удалено / EN: Deleted"
// @Failure 400 {string} string "RU: Не передан обязательный параметр / EN: Required parameter is missing"
// @Failure 404 {string} string "RU: Элемент не найден / EN: Item not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /dictionaries/item [delete]
func (h *Handler) DeleteDictionaryItem(w http.ResponseWriter, r *http.Request) {
	business, dictionaryCode, itemCode, ok := dictionaryItemKeyFromRequest(w, r)
	if !ok {
		return
	}

	if err := h.aggregator.DeleteDictionaryItem(r.Context(), business, dictionaryCode, itemCode); err != nil {
		h.writeDictionaryItemError(w, business, dictionaryCode, itemCode, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetAppInfo godoc
// @Summary RU: Получить информацию о приложении / EN: Get application info
// @Description RU: Возвращает имя приложения, версию, Git-метаданные и текущее время сервера. EN: Returns application name, version, Git metadata, and current server time.
// @Tags info
// @Produce json
// @Success 200 {object} map[string]string "RU: Информация о приложении / EN: App info"
// @Router /info [get]
func (h *Handler) GetAppInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]string{
		"app_name":   version.AppName,
		"version":    version.Version,
		"git_commit": version.GitCommit,
		"git_branch": version.GitBranch,
		"time":       time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

type ExecuteQueryRequest struct {
	QueryID      string   `json:"query_id"`
	Async        *bool    `json:"async,omitempty"`
	Offset       int      `json:"offset"`
	Limit        int      `json:"limit"`
	StartRepDate string   `json:"start_rep_date,omitempty"`
	EndRepDate   string   `json:"end_rep_date,omitempty"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Visibility   string   `json:"visibility,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	OrgCode      string   `json:"org_code,omitempty"`
}

type ExecuteQueryAcceptedResponse struct {
	Status      string               `json:"status"`
	QueryID     string               `json:"query_id"`
	ExecutionID string               `json:"execution_id"`
	Execution   model.QueryExecution `json:"execution"`
	Message     string               `json:"message"`
}

type ExecuteQuerySyncResponse struct {
	Status    string               `json:"status"`
	QueryID   string               `json:"query_id"`
	Execution model.QueryExecution `json:"execution"`
	Result    model.QueryResult    `json:"result"`
}

// ExecuteQuery godoc
// @Summary RU: Запустить выполнение сохраненного запроса / EN: Start saved query execution
// @Description RU: Запускает запрос к СВАП по сохраненному шаблону. По умолчанию async=true: возвращает задачу со статусом queued, результат появится после завершения в /results. При async=false дожидается выполнения и возвращает результат. EN: Starts a saved query template execution. By default async=true: returns a queued execution job, and the result appears later in /results. With async=false waits for completion and returns the result.
// @Tags queries
// @Accept json
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param request body ExecuteQueryRequest true "RU: Параметры выполнения / EN: Execution parameters"
// @Success 202 {object} ExecuteQueryAcceptedResponse "RU: Выполнение принято / EN: Execution accepted"
// @Success 200 {object} ExecuteQuerySyncResponse "RU: Выполнение завершено синхронно / EN: Execution completed synchronously"
// @Failure 400 {object} ErrorResponse "RU: Некорректное тело запроса / EN: Invalid request body"
// @Failure 401 {object} ErrorResponse "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {object} ErrorResponse "RU: Запрос принадлежит другому пользователю / EN: Query belongs to another user"
// @Failure 404 {object} ErrorResponse "RU: Запрос не найден / EN: Query not found"
// @Failure 500 {object} ErrorResponse "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /query/execute [post]
func (h *Handler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}
	var req ExecuteQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateExecuteQueryRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	opts := queryExecutionOptionsFromRequest(req)
	opts.UserID = userID
	if shouldRunAsync(req) {
		execution, err := h.aggregator.StartQueryExecutionAsync(r.Context(), opts)
		if err != nil {
			h.writeQueryExecutionError(w, req.QueryID, err)
			return
		}

		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(ExecuteQueryAcceptedResponse{
			Status:      "accepted",
			QueryID:     req.QueryID,
			ExecutionID: execution.ID,
			Execution:   execution,
			Message:     "query execution has been accepted; check /api/v1/query/executions or /api/v1/results for completion",
		})
		return
	}

	execution, result, err := h.aggregator.RunQueryExecutionSync(r.Context(), opts)
	if err != nil {
		h.writeQueryExecutionError(w, req.QueryID, err)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ExecuteQuerySyncResponse{
		Status:    "succeeded",
		QueryID:   req.QueryID,
		Execution: execution,
		Result:    result,
	})
}

// ListQueryExecutions godoc
// @Summary RU: Получить задачи выполнения пользователя / EN: List user query executions
// @Description RU: Возвращает статусы запусков запросов пользователя из заголовка X-User-ID. EN: Returns query execution statuses for the user from the X-User-ID header.
// @Tags queries
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Success 200 {array} model.QueryExecution "RU: Список задач выполнения / EN: Query execution list"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /query/executions [get]
func (h *Handler) ListQueryExecutions(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}

	executions, err := h.aggregator.GetQueryExecutionsByUser(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to list query executions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(executions)
}

// GetQueryExecution godoc
// @Summary RU: Получить статус задачи выполнения / EN: Get query execution status
// @Description RU: Возвращает статус конкретного запуска запроса. EN: Returns the status of a specific query execution.
// @Tags queries
// @Produce json
// @Param X-User-ID header string true "RU: Идентификатор пользователя / EN: User identifier"
// @Param id path string true "RU: UUID задачи выполнения / EN: Execution UUID"
// @Success 200 {object} model.QueryExecution "RU: Задача выполнения / EN: Query execution"
// @Failure 400 {string} string "RU: Некорректный UUID задачи / EN: Invalid execution UUID"
// @Failure 401 {string} string "RU: Не передан X-User-ID / EN: X-User-ID is missing"
// @Failure 403 {string} string "RU: Задача принадлежит другому пользователю / EN: Execution belongs to another user"
// @Failure 404 {string} string "RU: Задача не найдена / EN: Execution not found"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /query/executions/{id} [get]
func (h *Handler) GetQueryExecution(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		http.Error(w, "missing X-User-ID header", http.StatusUnauthorized)
		return
	}

	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid execution id", http.StatusBadRequest)
		return
	}

	execution, err := h.aggregator.GetQueryExecutionByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrQueryExecutionNotFound) {
			http.Error(w, "query execution with id "+id+" was not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to get query execution: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if execution.UserID != userID {
		http.Error(w, "query execution belongs to another user", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(execution)
}

func (h *Handler) writeQueryExecutionError(w http.ResponseWriter, queryID string, err error) {
	if errors.Is(err, service.ErrSavedQueryNotFound) {
		http.Error(w, "saved query with id "+queryID+" was not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		http.Error(w, "saved query belongs to another user", http.StatusForbidden)
		return
	}
	http.Error(w, "failed to execute query: "+err.Error(), http.StatusInternalServerError)
}

func (h *Handler) writeSavedQueryAccessError(w http.ResponseWriter, queryID string, err error) {
	if errors.Is(err, service.ErrSavedQueryNotFound) {
		http.Error(w, "saved query with id "+queryID+" was not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		http.Error(w, "saved query belongs to another user", http.StatusForbidden)
		return
	}
	http.Error(w, "failed to check saved query access: "+err.Error(), http.StatusInternalServerError)
}

func (h *Handler) writeQueryResultAccessError(w http.ResponseWriter, resultID string, err error) {
	if errors.Is(err, service.ErrQueryResultNotFound) {
		http.Error(w, "query result with id "+resultID+" was not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, service.ErrSavedQueryNotFound) {
		http.Error(w, "saved query for result "+resultID+" was not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		http.Error(w, "query result belongs to another user", http.StatusForbidden)
		return
	}
	http.Error(w, "failed to check query result access: "+err.Error(), http.StatusInternalServerError)
}

func queryExecutionOptionsFromRequest(req ExecuteQueryRequest) service.QueryExecutionOptions {
	return service.QueryExecutionOptions{
		QueryID:      req.QueryID,
		Offset:       req.Offset,
		Limit:        req.Limit,
		StartRepDate: req.StartRepDate,
		EndRepDate:   req.EndRepDate,
		Name:         req.Name,
		Description:  req.Description,
		Visibility:   req.Visibility,
		Roles:        req.Roles,
		OrgCode:      req.OrgCode,
	}
}

func shouldRunAsync(req ExecuteQueryRequest) bool {
	if req.Async == nil {
		return true
	}
	return *req.Async
}

// GetConfig godoc
// @Summary RU: Получить текущую подготовленную конфигурацию / EN: Get current pending configuration
// @Description RU: Возвращает всю подготовленную конфигурацию, включая еще не примененные изменения. EN: Returns the full pending configuration, including unapplied changes.
// @Tags config
// @Produce json
// @Success 200 {object} config.AppConfig "RU: Подготовленная конфигурация / EN: Pending configuration"
// @Router /config [get]
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.config.GetPending()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// ListConfigGroups godoc
// @Summary RU: Получить группы конфигурации / EN: List configuration groups
// @Description RU: Возвращает имена доступных групп конфигурации. EN: Returns names of available configuration groups.
// @Tags config
// @Produce json
// @Success 200 {array} string "RU: Группы конфигурации / EN: Configuration groups"
// @Router /config/groups [get]
func (h *Handler) ListConfigGroups(w http.ResponseWriter, r *http.Request) {
	groups := []string{config.GroupServer, config.GroupSVAP, config.GroupRetention}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

// GetConfigGroup godoc
// @Summary RU: Получить группу конфигурации / EN: Get a configuration group
// @Description RU: Возвращает настройки указанной группы: server, svap или retention. EN: Returns settings for the specified group: server, svap, or retention.
// @Tags config
// @Produce json
// @Param name path string true "RU: Имя группы / EN: Group name"
// @Success 200 {object} any "RU: Настройки группы / EN: Group settings"
// @Failure 404 {string} string "RU: Группа не найдена / EN: Group not found"
// @Router /config/groups/{name} [get]
func (h *Handler) GetConfigGroup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg := h.config.GetPending()

	var groupData any
	switch name {
	case config.GroupServer:
		groupData = cfg.Server
	case config.GroupSVAP:
		groupData = cfg.SVAP
	case config.GroupRetention:
		groupData = cfg.Retention
	default:
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groupData)
}

// UpdateConfig godoc
// @Summary RU: Обновить подготовленную конфигурацию / EN: Update pending configuration
// @Description RU: Частично или полностью обновляет подготовленную конфигурацию из JSON-тела без немедленного применения. EN: Partially or fully updates the pending configuration from the JSON body without applying it immediately.
// @Tags config
// @Accept json
// @Param config body any true "RU: Частичный или полный JSON конфигурации / EN: Partial or full configuration JSON"
// @Success 204 "RU: Обновлено, тело ответа отсутствует / EN: Updated, no response body"
// @Failure 400 {string} string "RU: Некорректное тело запроса / EN: Invalid request body"
// @Router /config [put]
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if err := h.config.UpdatePendingFromRaw(data); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ApplyConfig godoc
// @Summary RU: Применить подготовленную конфигурацию / EN: Apply pending configuration
// @Description RU: Сохраняет подготовленную конфигурацию в БД и отправляет уведомление узлам через PostgreSQL NOTIFY. EN: Persists the pending configuration to the database and notifies nodes through PostgreSQL NOTIFY.
// @Tags config
// @Success 200 "RU: Конфигурация применена / EN: Configuration applied"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /config/apply [post]
func (h *Handler) ApplyConfig(w http.ResponseWriter, r *http.Request) {
	if err := h.config.Apply(r.Context()); err != nil {
		http.Error(w, "failed to apply config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func userIDFromRequest(r *http.Request) (string, bool) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	return userID, userID != ""
}

func dictionaryItemKeyFromRequest(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	business := strings.TrimSpace(r.URL.Query().Get("business"))
	dictionaryCode := strings.TrimSpace(r.URL.Query().Get("dictionary_code"))
	itemCode := strings.TrimSpace(r.URL.Query().Get("item_code"))
	if business == "" {
		http.Error(w, "business is required", http.StatusBadRequest)
		return "", "", "", false
	}
	if dictionaryCode == "" {
		http.Error(w, "dictionary_code is required", http.StatusBadRequest)
		return "", "", "", false
	}
	if itemCode == "" {
		http.Error(w, "item_code is required", http.StatusBadRequest)
		return "", "", "", false
	}
	return business, dictionaryCode, itemCode, true
}

func (h *Handler) writeDictionaryItemError(w http.ResponseWriter, business string, dictionaryCode string, itemCode string, err error) {
	if errors.Is(err, service.ErrDictionaryItemNotFound) {
		http.Error(w, "dictionary item with business "+business+", dictionary_code "+dictionaryCode+" and item_code "+itemCode+" was not found", http.StatusNotFound)
		return
	}
	http.Error(w, "failed to handle dictionary item: "+err.Error(), http.StatusInternalServerError)
}

func validateSavedQuery(q model.SavedQuery) error {
	if strings.TrimSpace(q.Name) == "" {
		return errBadRequest("name is required")
	}
	if strings.TrimSpace(q.QueryType) == "" {
		return errBadRequest("query_type is required")
	}
	if !isKnownQueryType(q.QueryType) {
		return errBadRequest("unsupported query_type")
	}
	if q.Visibility != "" && !isValidVisibility(q.Visibility) {
		return errBadRequest("invalid visibility value")
	}
	if !json.Valid([]byte(q.Params)) {
		return errBadRequest("params must be valid JSON")
	}
	return nil
}

func validateDictionaryItem(item model.DictionaryItem) error {
	if strings.TrimSpace(item.Business) == "" {
		return errBadRequest("business is required")
	}
	if strings.TrimSpace(item.DictionaryCode) == "" {
		return errBadRequest("dictionary_code is required")
	}
	if strings.TrimSpace(item.Code) == "" {
		return errBadRequest("code is required")
	}
	if strings.TrimSpace(item.ShortName) == "" {
		return errBadRequest("short_name is required")
	}
	if strings.TrimSpace(item.FullName) == "" {
		return errBadRequest("full_name is required")
	}
	return nil
}

func validateExecuteQueryRequest(req ExecuteQueryRequest) error {
	if _, err := uuid.Parse(req.QueryID); err != nil {
		return errBadRequest("invalid query_id")
	}
	if req.Offset < 0 {
		return errBadRequest("offset must be non-negative")
	}
	if req.Limit < 0 || req.Limit > maxPageLimit {
		return errBadRequest("invalid limit")
	}
	if req.Visibility != "" && !isValidVisibility(req.Visibility) {
		return errBadRequest("invalid visibility value")
	}
	if err := validateDate("start_rep_date", req.StartRepDate); err != nil {
		return err
	}
	if err := validateDate("end_rep_date", req.EndRepDate); err != nil {
		return err
	}
	return nil
}

func validateDate(field, value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return errBadRequest(field + " must use YYYY-MM-DD format")
	}
	return nil
}

func parseOptionalNonNegativeInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, errBadRequest("invalid integer")
	}
	return value, nil
}

func isValidVisibility(value string) bool {
	switch value {
	case model.VisibilityPrivate, model.VisibilityOrganization, model.VisibilityPublic:
		return true
	default:
		return false
	}
}

func isKnownQueryType(value string) bool {
	switch value {
	case "OPLIST", "FSG", "TURN", "ReportGK", "COR", "PA", "CONS":
		return true
	default:
		return false
	}
}

type errBadRequest string

func (e errBadRequest) Error() string {
	return string(e)
}
