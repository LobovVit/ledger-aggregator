package api

import (
	"encoding/json"
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
	mux.HandleFunc("GET /api/v1/results", h.ListResults)
	mux.HandleFunc("DELETE /api/v1/results/{id}", h.DeleteResult)
	mux.HandleFunc("GET /api/v1/results/{id}/data", h.GetResultData)

	// Info routes
	mux.HandleFunc("GET /api/v1/info", h.GetAppInfo)

	// Config routes
	mux.HandleFunc("GET /api/v1/config", h.GetConfig)
	mux.HandleFunc("GET /api/v1/config/groups", h.ListConfigGroups)
	mux.HandleFunc("GET /api/v1/config/groups/{name}", h.GetConfigGroup)
	mux.HandleFunc("PUT /api/v1/config", h.UpdateConfig)
	mux.HandleFunc("POST /api/v1/config/apply", h.ApplyConfig)
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
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 204 "RU: Удалено, тело ответа отсутствует / EN: Deleted, no response body"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries/{id} [delete]
func (h *Handler) DeleteQuery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.DeleteSavedQuery(r.Context(), id); err != nil {
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
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 200 {object} model.SavedQuery "RU: Сохраненный запрос / EN: Saved query"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 404 {string} string "RU: Запрос не найден / EN: Query not found"
// @Router /queries/{id} [get]
func (h *Handler) GetQuery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	q, err := h.aggregator.GetSavedQueryByID(r.Context(), id)
	if err != nil {
		http.Error(w, "query not found", http.StatusNotFound)
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
// @Param id path string true "RU: UUID запроса / EN: Query UUID"
// @Success 200 {array} model.QueryResult "RU: История результатов / EN: Query result history"
// @Failure 400 {string} string "RU: Некорректный UUID запроса / EN: Invalid query UUID"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /queries/{id}/results [get]
func (h *Handler) ListQueryResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid query id", http.StatusBadRequest)
		return
	}
	results, err := h.aggregator.GetQueryResults(r.Context(), id)
	if err != nil {
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
// @Param id path string true "RU: UUID результата / EN: Result UUID"
// @Success 204 "RU: Удалено, тело ответа отсутствует / EN: Deleted, no response body"
// @Failure 400 {string} string "RU: Некорректный UUID результата / EN: Invalid result UUID"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /results/{id} [delete]
func (h *Handler) DeleteResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid result id", http.StatusBadRequest)
		return
	}
	if err := h.aggregator.DeleteQueryResult(r.Context(), id); err != nil {
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
// @Param id path string true "RU: UUID результата / EN: Result UUID"
// @Param limit query int false "RU: Количество строк, максимум 1000 / EN: Number of rows to return, max 1000"
// @Param offset query int false "RU: Количество строк для пропуска / EN: Number of rows to skip"
// @Success 200 {array} map[string]any "RU: Плоские строки результата / EN: Flat result rows"
// @Failure 400 {string} string "RU: Некорректный UUID, limit или offset / EN: Invalid UUID, limit, or offset"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /results/{id}/data [get]
func (h *Handler) GetResultData(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "invalid result id", http.StatusBadRequest)
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
		http.Error(w, "failed to get result data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
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

// ExecuteQuery godoc
// @Summary RU: Выполнить сохраненный запрос / EN: Execute a saved query
// @Description RU: Выполняет запрос к СВАП по сохраненному шаблону, сохраняет результат и возвращает его метаданные. EN: Executes a saved query template against SVAP, stores the result, and returns its metadata.
// @Tags queries
// @Accept json
// @Produce json
// @Param request body ExecuteQueryRequest true "RU: Параметры выполнения / EN: Execution parameters"
// @Success 200 {object} model.QueryResult "RU: Метаданные сохраненного результата / EN: Stored result metadata"
// @Failure 400 {string} string "RU: Некорректное тело запроса / EN: Invalid request body"
// @Failure 500 {string} string "RU: Внутренняя ошибка сервера / EN: Internal server error"
// @Router /query/execute [post]
func (h *Handler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req ExecuteQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := validateExecuteQueryRequest(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := h.aggregator.ExecuteAndSaveQuery(r.Context(), req.QueryID, req.Offset, req.Limit, req.StartRepDate, req.EndRepDate, req.Name, req.Description, req.Visibility, req.Roles, req.OrgCode)
	if err != nil {
		http.Error(w, "failed to execute query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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
