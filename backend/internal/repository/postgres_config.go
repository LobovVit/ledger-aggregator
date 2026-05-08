package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type PostgresConfigRepository struct {
	db *sql.DB
}

func NewPostgresConfigRepository(db *sql.DB) *PostgresConfigRepository {
	return &PostgresConfigRepository{db: db}
}

func (r *PostgresConfigRepository) Save(ctx context.Context, groupName string, cfg any) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		INSERT INTO app_config (group_name, value, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (group_name) DO UPDATE
		SET value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP
	`
	_, err = r.db.ExecContext(ctx, query, groupName, data)
	if err != nil {
		return fmt.Errorf("failed to exec save query: %w", err)
	}

	return nil
}

func (r *PostgresConfigRepository) Load(ctx context.Context, groupName string) (any, error) {
	var data []byte
	query := `SELECT value FROM app_config WHERE group_name = $1`
	err := r.db.QueryRowContext(ctx, query, groupName).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load config group %s: %w", groupName, err)
	}

	var res any
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return res, nil
}

func (r *PostgresConfigRepository) LoadAll(ctx context.Context) (map[string]any, error) {
	query := `SELECT group_name, value FROM app_config`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all configs: %w", err)
	}
	defer rows.Close()

	res := make(map[string]any)
	for rows.Next() {
		var groupName string
		var data []byte
		if err := rows.Scan(&groupName, &data); err != nil {
			return nil, fmt.Errorf("failed to scan config row: %w", err)
		}

		var val any
		if err := json.Unmarshal(data, &val); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config value for %s: %w", groupName, err)
		}
		res[groupName] = val
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return res, nil
}
