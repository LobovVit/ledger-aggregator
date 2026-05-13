package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"ledger-aggregator/backend/internal/model"

	"github.com/lib/pq"
)

type PostgresSavedQueryRepository struct {
	db *sql.DB
}

func NewPostgresSavedQueryRepository(db *sql.DB) *PostgresSavedQueryRepository {
	return &PostgresSavedQueryRepository{db: db}
}

func (r *PostgresSavedQueryRepository) Save(ctx context.Context, q model.SavedQuery) (string, error) {
	if q.ID != "" {
		query := `
			INSERT INTO saved_queries (id, user_id, name, description, visibility, query_type, params, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`
		var id string
		err := r.db.QueryRowContext(ctx, query, q.ID, q.UserID, q.Name, q.Description, q.Visibility, q.QueryType, q.Params, q.CreatedAt).Scan(&id)
		return id, err
	}

	query := `
		INSERT INTO saved_queries (user_id, name, description, visibility, query_type, params, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`
	var id string
	err := r.db.QueryRowContext(ctx, query, q.UserID, q.Name, q.Description, q.Visibility, q.QueryType, q.Params, q.CreatedAt).Scan(&id)
	return id, err
}

func (r *PostgresSavedQueryRepository) GetByID(ctx context.Context, id string) (model.SavedQuery, error) {
	query := `
		SELECT id, user_id, name, description, visibility, query_type, params, created_at
		FROM saved_queries WHERE id = $1
	`
	var q model.SavedQuery
	err := r.db.QueryRowContext(ctx, query, id).Scan(&q.ID, &q.UserID, &q.Name, &q.Description, &q.Visibility, &q.QueryType, &q.Params, &q.CreatedAt)
	if err != nil {
		return model.SavedQuery{}, err
	}
	return q, nil
}

func (r *PostgresSavedQueryRepository) GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error) {
	query := `
		SELECT id, user_id, name, description, visibility, query_type, params, created_at
		FROM saved_queries WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []model.SavedQuery
	for rows.Next() {
		var q model.SavedQuery
		if err := rows.Scan(&q.ID, &q.UserID, &q.Name, &q.Description, &q.Visibility, &q.QueryType, &q.Params, &q.CreatedAt); err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return queries, nil
}

func (r *PostgresSavedQueryRepository) GetAll(ctx context.Context) ([]model.SavedQuery, error) {
	query := `
		SELECT id, user_id, name, description, visibility, query_type, params, created_at
		FROM saved_queries
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []model.SavedQuery
	for rows.Next() {
		var q model.SavedQuery
		if err := rows.Scan(&q.ID, &q.UserID, &q.Name, &q.Description, &q.Visibility, &q.QueryType, &q.Params, &q.CreatedAt); err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return queries, nil
}

func (r *PostgresSavedQueryRepository) Update(ctx context.Context, q model.SavedQuery) error {
	query := `
		UPDATE saved_queries
		SET user_id = $2, name = $3, description = $4, visibility = $5, query_type = $6, params = $7
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, q.ID, q.UserID, q.Name, q.Description, q.Visibility, q.QueryType, q.Params)
	return err
}

func (r *PostgresSavedQueryRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM query_results WHERE query_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM saved_queries WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type PostgresQueryResultRepository struct {
	db *sql.DB
}

func NewPostgresQueryResultRepository(db *sql.DB) *PostgresQueryResultRepository {
	return &PostgresQueryResultRepository{db: db}
}

func (r *PostgresQueryResultRepository) Save(ctx context.Context, res model.QueryResult, rows []model.QueryResultRow, values []model.QueryResultValue) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// 1. Save result metadata
	metaJSON, err := json.Marshal(res.Meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal meta: %w", err)
	}

	var resultID string
	if res.ID != "" {
		err = tx.QueryRowContext(ctx, "INSERT INTO query_results (id, query_id, name, description, visibility, meta, fetched_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id",
			res.ID, res.QueryID, res.Name, res.Description, res.Visibility, metaJSON, res.FetchedAt, res.ExpiresAt).Scan(&resultID)
	} else {
		err = tx.QueryRowContext(ctx, "INSERT INTO query_results (query_id, name, description, visibility, meta, fetched_at, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id",
			res.QueryID, res.Name, res.Description, res.Visibility, metaJSON, res.FetchedAt, res.ExpiresAt).Scan(&resultID)
	}
	if err != nil {
		return "", err
	}

	// Map to track generated row IDs if we are not passing them
	rowIDMap := make(map[string]string)

	// 2. Save rows
	for i, row := range rows {
		var generatedRowID string
		if row.ID != "" {
			err = tx.QueryRowContext(ctx, "INSERT INTO query_result_rows (id, result_id, created_at) VALUES ($1, $2, $3) RETURNING id",
				row.ID, resultID, row.CreatedAt).Scan(&generatedRowID)
		} else {
			err = tx.QueryRowContext(ctx, "INSERT INTO query_result_rows (result_id, created_at) VALUES ($1, $2) RETURNING id",
				resultID, row.CreatedAt).Scan(&generatedRowID)
		}
		if err != nil {
			return "", err
		}
		if row.ID != "" {
			rowIDMap[row.ID] = generatedRowID
		} else {
			// If original ID was empty, we need some way to link values.
			// But AggregatorService currently populates row.ID.
			// We'll handle both cases.
		}
		rows[i].ID = generatedRowID
	}

	// 3. Save values
	for _, val := range values {
		rowID := val.RowID
		// If we generated a new ID for the row, use it
		if mappedID, ok := rowIDMap[val.RowID]; ok {
			rowID = mappedID
		}

		_, err = tx.ExecContext(ctx, "INSERT INTO query_row_values (row_id, attribute_code, attribute_value, numeric_value) VALUES ($1, $2, $3, $4)",
			rowID, val.AttributeCode, val.AttributeValue, val.NumericValue)
		if err != nil {
			return "", err
		}
	}

	return resultID, tx.Commit()
}

func (r *PostgresQueryResultRepository) GetByQueryID(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	query := `SELECT id, query_id, name, description, visibility, meta, fetched_at, expires_at FROM query_results WHERE query_id = $1 ORDER BY fetched_at DESC`
	rows, err := r.db.QueryContext(ctx, query, queryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []model.QueryResult
	for rows.Next() {
		var res model.QueryResult
		var metaData []byte
		if err := rows.Scan(&res.ID, &res.QueryID, &res.Name, &res.Description, &res.Visibility, &metaData, &res.FetchedAt, &res.ExpiresAt); err != nil {
			return nil, err
		}
		if len(metaData) > 0 {
			if err := json.Unmarshal(metaData, &res.Meta); err != nil {
				return nil, err
			}
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *PostgresQueryResultRepository) GetByUserID(ctx context.Context, userID string) ([]model.QueryResult, error) {
	query := `
		SELECT qr.id, qr.query_id, qr.name, qr.description, qr.visibility, qr.meta, qr.fetched_at, qr.expires_at
		FROM query_results qr
		JOIN saved_queries sq ON sq.id = qr.query_id
		WHERE sq.user_id = $1
		ORDER BY qr.fetched_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []model.QueryResult
	for rows.Next() {
		var res model.QueryResult
		var metaData []byte
		if err := rows.Scan(&res.ID, &res.QueryID, &res.Name, &res.Description, &res.Visibility, &metaData, &res.FetchedAt, &res.ExpiresAt); err != nil {
			return nil, err
		}
		if len(metaData) > 0 {
			if err := json.Unmarshal(metaData, &res.Meta); err != nil {
				return nil, err
			}
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *PostgresQueryResultRepository) GetAll(ctx context.Context) ([]model.QueryResult, error) {
	query := `SELECT id, query_id, name, description, visibility, meta, fetched_at, expires_at FROM query_results ORDER BY fetched_at DESC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []model.QueryResult
	for rows.Next() {
		var res model.QueryResult
		var metaData []byte
		if err := rows.Scan(&res.ID, &res.QueryID, &res.Name, &res.Description, &res.Visibility, &metaData, &res.FetchedAt, &res.ExpiresAt); err != nil {
			return nil, err
		}
		if len(metaData) > 0 {
			if err := json.Unmarshal(metaData, &res.Meta); err != nil {
				return nil, err
			}
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *PostgresQueryResultRepository) GetByID(ctx context.Context, id string) (model.QueryResult, error) {
	query := `SELECT id, query_id, name, description, visibility, meta, fetched_at, expires_at FROM query_results WHERE id = $1`
	var res model.QueryResult
	var metaData []byte
	err := r.db.QueryRowContext(ctx, query, id).Scan(&res.ID, &res.QueryID, &res.Name, &res.Description, &res.Visibility, &metaData, &res.FetchedAt, &res.ExpiresAt)
	if err != nil {
		return model.QueryResult{}, err
	}
	if len(metaData) > 0 {
		if err := json.Unmarshal(metaData, &res.Meta); err != nil {
			return model.QueryResult{}, err
		}
	}
	return res, nil
}

func (r *PostgresQueryResultRepository) GetRows(ctx context.Context, resultID string) ([]model.QueryResultRow, error) {
	query := `SELECT id, result_id, created_at FROM query_result_rows WHERE result_id = $1`
	rows, err := r.db.QueryContext(ctx, query, resultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resRows []model.QueryResultRow
	for rows.Next() {
		var row model.QueryResultRow
		if err := rows.Scan(&row.ID, &row.ResultID, &row.CreatedAt); err != nil {
			return nil, err
		}
		resRows = append(resRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return resRows, nil
}

func (r *PostgresQueryResultRepository) GetValuesByRowID(ctx context.Context, rowID string) ([]model.QueryResultValue, error) {
	query := `SELECT id, row_id, attribute_code, attribute_value, numeric_value FROM query_row_values WHERE row_id = $1`
	rows, err := r.db.QueryContext(ctx, query, rowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []model.QueryResultValue
	for rows.Next() {
		var val model.QueryResultValue
		if err := rows.Scan(&val.ID, &val.RowID, &val.AttributeCode, &val.AttributeValue, &val.NumericValue); err != nil {
			return nil, err
		}
		values = append(values, val)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func (r *PostgresQueryResultRepository) GetFullResultData(ctx context.Context, resultID string, offset, limit int) ([]map[string]any, error) {
	// Сначала выбираем ID строк с учетом пагинации
	rowsQuery := `SELECT id FROM query_result_rows WHERE result_id = $1 ORDER BY id`
	args := []any{resultID}
	if limit > 0 {
		args = append(args, limit)
		rowsQuery += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		rowsQuery += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rowsRows, err := r.db.QueryContext(ctx, rowsQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rowsRows.Close()

	var pagedRowIDs []string
	for rowsRows.Next() {
		var id string
		if err := rowsRows.Scan(&id); err != nil {
			return nil, err
		}
		pagedRowIDs = append(pagedRowIDs, id)
	}
	if err := rowsRows.Err(); err != nil {
		return nil, err
	}

	if len(pagedRowIDs) == 0 {
		return []map[string]any{}, nil
	}

	// Теперь получаем значения для этих конкретных строк
	// Используем ANY($1) для передачи слайса ID в SQL
	valuesQuery := `
		SELECT row_id, attribute_code, attribute_value, numeric_value
		FROM query_row_values
		WHERE row_id = ANY($1)
		ORDER BY row_id, id
	`
	rows, err := r.db.QueryContext(ctx, valuesQuery, pq.Array(pagedRowIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dataMap := make(map[string]map[string]any)
	for rows.Next() {
		var rowID, code string
		var valStr sql.NullString
		var valNum sql.NullFloat64
		if err := rows.Scan(&rowID, &code, &valStr, &valNum); err != nil {
			return nil, err
		}

		if _, ok := dataMap[rowID]; !ok {
			dataMap[rowID] = make(map[string]any)
		}

		if valNum.Valid {
			dataMap[rowID][code] = valNum.Float64
		} else if valStr.Valid {
			dataMap[rowID][code] = valStr.String
		} else {
			dataMap[rowID][code] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(pagedRowIDs))
	for _, id := range pagedRowIDs {
		if data, ok := dataMap[id]; ok {
			result = append(result, data)
		} else {
			// Если для строки нет значений (редкий случай, но возможный)
			result = append(result, map[string]any{})
		}
	}

	return result, nil
}

func (r *PostgresQueryResultRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM query_results WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
