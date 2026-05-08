package api

import (
	"encoding/json"
	"io"
	"net/http"

	"ledger-aggregator/backend/internal/config"
	"ledger-aggregator/backend/internal/model"
	"ledger-aggregator/backend/internal/service"
)

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
	mux.HandleFunc("GET /api/v1/queries/{id}/results", h.ListQueryResults)
	mux.HandleFunc("GET /api/v1/results/{id}/data", h.GetResultData)

	// Config routes
	mux.HandleFunc("GET /api/v1/config", h.GetConfig)
	mux.HandleFunc("GET /api/v1/config/groups", h.ListConfigGroups)
	mux.HandleFunc("GET /api/v1/config/groups/{name}", h.GetConfigGroup)
	mux.HandleFunc("PUT /api/v1/config", h.UpdateConfig)
	mux.HandleFunc("POST /api/v1/config/apply", h.ApplyConfig)
}

func (h *Handler) CreateQuery(w http.ResponseWriter, r *http.Request) {
	var q model.SavedQuery
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	// В реальности userID берется из токена/контекста
	if q.UserID == "" {
		q.UserID = "default-user"
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

func (h *Handler) ListQueries(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "default-user"
	}

	queries, err := h.aggregator.GetSavedQueries(r.Context(), userID)
	if err != nil {
		http.Error(w, "failed to list queries: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(queries)
}

func (h *Handler) GetQuery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q, err := h.aggregator.GetSavedQueryByID(r.Context(), id)
	if err != nil {
		http.Error(w, "query not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

func (h *Handler) ListQueryResults(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	results, err := h.aggregator.GetQueryResults(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to list results: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (h *Handler) GetResultData(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, err := h.aggregator.GetResultData(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to get result data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

type ExecuteQueryRequest struct {
	QueryID string `json:"query_id"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

func (h *Handler) ExecuteQuery(w http.ResponseWriter, r *http.Request) {
	var req ExecuteQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.aggregator.ExecuteAndSaveQuery(r.Context(), req.QueryID, req.Offset, req.Limit)
	if err != nil {
		http.Error(w, "failed to execute query: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.config.GetPending()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (h *Handler) ListConfigGroups(w http.ResponseWriter, r *http.Request) {
	groups := []string{config.GroupServer, config.GroupSVAP, config.GroupRetention}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(groups)
}

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

func (h *Handler) ApplyConfig(w http.ResponseWriter, r *http.Request) {
	if err := h.config.Apply(r.Context()); err != nil {
		http.Error(w, "failed to apply config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
