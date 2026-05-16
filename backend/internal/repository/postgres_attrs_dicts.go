package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"svap-query-service/backend/internal/model"

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
		attr, err := scanAnalyticalAttribute(rows)
		if err != nil {
			return nil, err
		}
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
		attr, err := scanAnalyticalAttribute(rows)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return attrs, nil
}

type analyticalAttributeScanner interface {
	Scan(dest ...any) error
}

func scanAnalyticalAttribute(row analyticalAttributeScanner) (model.AnalyticalAttribute, error) {
	var attr model.AnalyticalAttribute
	var businesses pq.StringArray
	var validationType sql.NullString
	var validationValue sql.NullString
	err := row.Scan(
		&attr.Code,
		&attr.Name,
		&businesses,
		&attr.InAccount,
		&attr.UseInBalance,
		&validationType,
		&validationValue,
		&attr.LastUpdated,
	)
	if err != nil {
		return model.AnalyticalAttribute{}, err
	}
	attr.Businesses = []string(businesses)
	if validationType.Valid {
		attr.ValidationType = validationType.String
	}
	if validationValue.Valid {
		attr.ValidationValue = validationValue.String
	}
	return attr, nil
}

type PostgresDictionaryCacheRepository struct {
	db *sql.DB
}

func NewPostgresDictionaryCacheRepository(db *sql.DB) *PostgresDictionaryCacheRepository {
	return &PostgresDictionaryCacheRepository{db: db}
}

func (r *PostgresDictionaryCacheRepository) Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error) {
	item, err := r.GetItem(ctx, business, dictionaryCode, key)
	if err != nil {
		return "", err
	}
	return item.ShortName, nil
}

func (r *PostgresDictionaryCacheRepository) Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error {
	_, err := r.UpsertItem(ctx, model.DictionaryItem{
		Business:       business,
		DictionaryCode: dictionaryCode,
		Code:           key,
		ShortName:      value,
		FullName:       value,
	})
	return err
}

func (r *PostgresDictionaryCacheRepository) GetItem(ctx context.Context, business string, dictionaryCode string, itemCode string) (model.DictionaryItem, error) {
	query := `
		SELECT business, dictionary_code, item_code, item_short_name, item_full_name, analytical_attribute_code, last_updated
		FROM dictionaries_cache
		WHERE business = $1 AND dictionary_code = $2 AND item_code = $3
	`
	row := r.db.QueryRowContext(ctx, query, business, dictionaryCode, itemCode)
	return scanDictionaryItem(row)
}

func (r *PostgresDictionaryCacheRepository) UpsertItem(ctx context.Context, item model.DictionaryItem) (model.DictionaryItem, error) {
	query := `
		INSERT INTO dictionaries_cache (business, dictionary_code, item_code, item_short_name, item_full_name, analytical_attribute_code, last_updated)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), CURRENT_TIMESTAMP)
		ON CONFLICT (business, dictionary_code, item_code) DO UPDATE SET
			item_short_name = EXCLUDED.item_short_name,
			item_full_name = EXCLUDED.item_full_name,
			analytical_attribute_code = EXCLUDED.analytical_attribute_code,
			last_updated = CURRENT_TIMESTAMP
		RETURNING business, dictionary_code, item_code, item_short_name, item_full_name, analytical_attribute_code, last_updated
	`
	row := r.db.QueryRowContext(ctx, query, item.Business, item.DictionaryCode, item.Code, item.ShortName, item.FullName, item.AnalyticalAttributeCode)
	return scanDictionaryItem(row)
}

func (r *PostgresDictionaryCacheRepository) DeleteItem(ctx context.Context, business string, dictionaryCode string, itemCode string) error {
	query := `DELETE FROM dictionaries_cache WHERE business = $1 AND dictionary_code = $2 AND item_code = $3`
	res, err := r.db.ExecContext(ctx, query, business, dictionaryCode, itemCode)
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

func (r *PostgresDictionaryCacheRepository) Search(ctx context.Context, filter model.DictionaryFilter) ([]model.DictionaryItem, error) {
	filter = normalizeDictionaryFilter(filter)
	sqlQuery := `
		SELECT business, dictionary_code, item_code, item_short_name, item_full_name, analytical_attribute_code, last_updated
		FROM dictionaries_cache
		WHERE ($1 = '' OR business = $1)
		  AND ($2 = '' OR dictionary_code = $2)
		  AND ($3 = '' OR item_short_name ILIKE '%' || $3 || '%' OR item_full_name ILIKE '%' || $3 || '%')
		  AND ($4 = '' OR analytical_attribute_code = $4)
		ORDER BY
		    business,
		    dictionary_code,
		    CASE WHEN dictionary_code LIKE 'SVAP_QUERY_%' THEN id ELSE NULL END,
		    item_code
	`
	rows, err := r.db.QueryContext(ctx, sqlQuery, filter.Business, filter.DictionaryCode, filter.Query, filter.AnalyticalAttributeCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.DictionaryItem
	for rows.Next() {
		item, err := scanDictionaryItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type dictionaryItemScanner interface {
	Scan(dest ...any) error
}

func scanDictionaryItem(row dictionaryItemScanner) (model.DictionaryItem, error) {
	var item model.DictionaryItem
	var analyticalAttributeCode sql.NullString
	err := row.Scan(
		&item.Business,
		&item.DictionaryCode,
		&item.Code,
		&item.ShortName,
		&item.FullName,
		&analyticalAttributeCode,
		&item.LastUpdated,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DictionaryItem{}, ErrNotFound
		}
		return model.DictionaryItem{}, err
	}
	if analyticalAttributeCode.Valid {
		item.AnalyticalAttributeCode = analyticalAttributeCode.String
	}
	return item, nil
}

func normalizeDictionaryFilter(filter model.DictionaryFilter) model.DictionaryFilter {
	filter.Business = strings.TrimSpace(filter.Business)
	filter.DictionaryCode = strings.TrimSpace(filter.DictionaryCode)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.AnalyticalAttributeCode = strings.TrimSpace(filter.AnalyticalAttributeCode)
	return filter
}
