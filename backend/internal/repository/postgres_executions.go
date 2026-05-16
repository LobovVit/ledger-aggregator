package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"svap-query-service/backend/internal/model"
)

type PostgresQueryExecutionRepository struct {
	db *sql.DB
}

func NewPostgresQueryExecutionRepository(db *sql.DB) *PostgresQueryExecutionRepository {
	return &PostgresQueryExecutionRepository{db: db}
}

func (r *PostgresQueryExecutionRepository) Create(ctx context.Context, execution model.QueryExecution) (model.QueryExecution, error) {
	query := `
		INSERT INTO query_executions (query_id, user_id, status, mode, start_rep_date, end_rep_date, offset_rows, limit_rows, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, query_id, user_id, status, mode, start_rep_date, end_rep_date, offset_rows, limit_rows, result_id, error_message, created_at, started_at, finished_at
	`
	row := r.db.QueryRowContext(
		ctx,
		query,
		execution.QueryID,
		execution.UserID,
		execution.Status,
		execution.Mode,
		emptyStringToNull(execution.StartRepDate),
		emptyStringToNull(execution.EndRepDate),
		execution.Offset,
		execution.Limit,
		execution.CreatedAt,
	)
	return scanQueryExecution(row)
}

func (r *PostgresQueryExecutionRepository) MarkRunning(ctx context.Context, id string, startedAt time.Time) error {
	query := `
		UPDATE query_executions
		SET status = $2, started_at = $3, error_message = NULL
		WHERE id = $1
	`
	return execOne(ctx, r.db, query, id, model.QueryExecutionStatusRunning, startedAt)
}

func (r *PostgresQueryExecutionRepository) MarkSucceeded(ctx context.Context, id string, resultID string, finishedAt time.Time) error {
	query := `
		UPDATE query_executions
		SET status = $2, result_id = $3, error_message = NULL, finished_at = $4
		WHERE id = $1
	`
	return execOne(ctx, r.db, query, id, model.QueryExecutionStatusSucceeded, resultID, finishedAt)
}

func (r *PostgresQueryExecutionRepository) MarkFailed(ctx context.Context, id string, message string, finishedAt time.Time) error {
	query := `
		UPDATE query_executions
		SET status = $2, error_message = $3, finished_at = $4
		WHERE id = $1
	`
	return execOne(ctx, r.db, query, id, model.QueryExecutionStatusFailed, message, finishedAt)
}

func (r *PostgresQueryExecutionRepository) GetByID(ctx context.Context, id string) (model.QueryExecution, error) {
	query := `
		SELECT id, query_id, user_id, status, mode, start_rep_date, end_rep_date, offset_rows, limit_rows, result_id, error_message, created_at, started_at, finished_at
		FROM query_executions
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	return scanQueryExecution(row)
}

func (r *PostgresQueryExecutionRepository) GetByUserID(ctx context.Context, userID string) ([]model.QueryExecution, error) {
	query := `
		SELECT id, query_id, user_id, status, mode, start_rep_date, end_rep_date, offset_rows, limit_rows, result_id, error_message, created_at, started_at, finished_at
		FROM query_executions
		WHERE user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var executions []model.QueryExecution
	for rows.Next() {
		execution, err := scanQueryExecution(rows)
		if err != nil {
			return nil, err
		}
		executions = append(executions, execution)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return executions, nil
}

type queryExecutionScanner interface {
	Scan(dest ...any) error
}

func scanQueryExecution(row queryExecutionScanner) (model.QueryExecution, error) {
	var execution model.QueryExecution
	var startRepDate sql.NullString
	var endRepDate sql.NullString
	var resultID sql.NullString
	var errorMessage sql.NullString
	var startedAt sql.NullTime
	var finishedAt sql.NullTime

	err := row.Scan(
		&execution.ID,
		&execution.QueryID,
		&execution.UserID,
		&execution.Status,
		&execution.Mode,
		&startRepDate,
		&endRepDate,
		&execution.Offset,
		&execution.Limit,
		&resultID,
		&errorMessage,
		&execution.CreatedAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.QueryExecution{}, ErrNotFound
		}
		return model.QueryExecution{}, err
	}

	if startRepDate.Valid {
		execution.StartRepDate = startRepDate.String
	}
	if endRepDate.Valid {
		execution.EndRepDate = endRepDate.String
	}
	if resultID.Valid {
		execution.ResultID = resultID.String
	}
	if errorMessage.Valid {
		execution.ErrorMessage = errorMessage.String
	}
	if startedAt.Valid {
		execution.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		execution.FinishedAt = &finishedAt.Time
	}
	return execution, nil
}

func emptyStringToNull(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func execOne(ctx context.Context, db *sql.DB, query string, args ...any) error {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
