package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ledger-aggregator/backend/internal/model"

	"github.com/lib/pq"
)

type PostgresAnalyticalAttributeRepository struct {
	db *sql.DB
}

func NewPostgresAnalyticalAttributeRepository(db *sql.DB) *PostgresAnalyticalAttributeRepository {
	return &PostgresAnalyticalAttributeRepository{db: db}
}

func (r *PostgresAnalyticalAttributeRepository) SaveAll(ctx context.Context, attrs []model.AnalyticalAttribute) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO analytical_attributes (code, name, businesses, in_account, use_in_balance, validation_type, validation_value, last_updated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name,
			businesses = EXCLUDED.businesses,
			in_account = EXCLUDED.in_account,
			use_in_balance = EXCLUDED.use_in_balance,
			validation_type = EXCLUDED.validation_type,
			validation_value = EXCLUDED.validation_value,
			last_updated = EXCLUDED.last_updated
	`

	for _, attr := range attrs {
		_, err = tx.ExecContext(ctx, query,
			attr.Code, attr.Name, pq.Array(attr.Businesses), attr.InAccount,
			attr.UseInBalance, attr.ValidationType, attr.ValidationValue, time.Now())
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *PostgresAnalyticalAttributeRepository) GetAll(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	query := `SELECT code, name, businesses, in_account, use_in_balance, validation_type, validation_value, last_updated FROM analytical_attributes`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attrs []model.AnalyticalAttribute
	for rows.Next() {
		var attr model.AnalyticalAttribute
		var businesses pq.StringArray
		err := rows.Scan(&attr.Code, &attr.Name, &businesses, &attr.InAccount, &attr.UseInBalance, &attr.ValidationType, &attr.ValidationValue, &attr.LastUpdated)
		if err != nil {
			return nil, err
		}
		attr.Businesses = []string(businesses)
		attrs = append(attrs, attr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attrs, nil
}

func (r *PostgresAnalyticalAttributeRepository) GetByBusiness(ctx context.Context, business string) ([]model.AnalyticalAttribute, error) {
	query := `SELECT code, name, businesses, in_account, use_in_balance, validation_type, validation_value, last_updated 
              FROM analytical_attributes WHERE $1 = ANY(businesses)`
	rows, err := r.db.QueryContext(ctx, query, business)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attrs []model.AnalyticalAttribute
	for rows.Next() {
		var attr model.AnalyticalAttribute
		var businesses pq.StringArray
		err := rows.Scan(&attr.Code, &attr.Name, &businesses, &attr.InAccount, &attr.UseInBalance, &attr.ValidationType, &attr.ValidationValue, &attr.LastUpdated)
		if err != nil {
			return nil, err
		}
		attr.Businesses = []string(businesses)
		attrs = append(attrs, attr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attrs, nil
}

type PostgresDictionaryCacheRepository struct {
	db *sql.DB
}

func NewPostgresDictionaryCacheRepository(db *sql.DB) *PostgresDictionaryCacheRepository {
	return &PostgresDictionaryCacheRepository{db: db}
}

func (r *PostgresDictionaryCacheRepository) Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error) {
	var name string
	query := `SELECT item_name FROM dictionaries_cache WHERE business = $1 AND dictionary_code = $2 AND item_code = $3`
	err := r.db.QueryRowContext(ctx, query, business, dictionaryCode, key).Scan(&name)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("not found")
	}
	return name, err
}

func (r *PostgresDictionaryCacheRepository) Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error {
	query := `
		INSERT INTO dictionaries_cache (business, dictionary_code, item_code, item_name, last_updated)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT (business, dictionary_code, item_code) DO UPDATE SET
			item_name = EXCLUDED.item_name,
			last_updated = CURRENT_TIMESTAMP
	`
	_, err := r.db.ExecContext(ctx, query, business, dictionaryCode, key, value)
	return err
}

func (r *PostgresDictionaryCacheRepository) Search(ctx context.Context, business string, dictionaryCode string, query string) ([]model.DictionaryItem, error) {
	sqlQuery := `SELECT item_code, item_name FROM dictionaries_cache 
                 WHERE business = $1 AND dictionary_code = $2 AND (item_code ILIKE $3 OR item_name ILIKE $3)`
	rows, err := r.db.QueryContext(ctx, sqlQuery, business, dictionaryCode, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.DictionaryItem
	for rows.Next() {
		var item model.DictionaryItem
		if err := rows.Scan(&item.Code, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
