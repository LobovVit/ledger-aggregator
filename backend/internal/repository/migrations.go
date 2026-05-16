package repository

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lib/pq"
)

// MigrationRunner отвечает за выполнение SQL миграций
type MigrationRunner struct {
	db     *sql.DB
	schema string
}

type migrationQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type migrationConnection interface {
	migrationQuerier
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func NewMigrationRunner(db *sql.DB, schema ...string) *MigrationRunner {
	targetSchema := "svap_query_service"
	if len(schema) > 0 && strings.TrimSpace(schema[0]) != "" {
		targetSchema = strings.TrimSpace(schema[0])
	}
	return &MigrationRunner{db: db, schema: targetSchema}
}

// Run выполняет все новые миграции из указанной директории.
// Использует PostgreSQL Advisory Lock для предотвращения одновременного запуска несколькими узлами.
func (m *MigrationRunner) Run(ctx context.Context, migrationsDir string) error {
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to reserve db connection for advisory lock: %w", err)
	}
	defer conn.Close()

	lockKey := "svap-query-service:migrations:" + m.schema
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock(hashtext($1)::bigint)", lockKey); err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock(hashtext($1)::bigint)", lockKey)
	}()

	// 1. Создаем таблицу миграций, если её нет
	if err := m.ensureMigrationsTable(ctx, conn); err != nil {
		return fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	// ФИКС: Если мы используем PostgreSQL 17+, gen_random_uuid() доступен из коробки.
	// Но для старых версий или на всякий случай, убедимся что pgcrypto доступен.
	if _, err := conn.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto;"); err != nil {
		fmt.Printf("Warning: failed to enable pgcrypto: %v\n", err)
	}

	// 2. Получаем список файлов миграций
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrationFiles []string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			migrationFiles = append(migrationFiles, f.Name())
		}
	}
	sort.Strings(migrationFiles)

	// 3. Получаем уже выполненные миграции
	executed, err := m.getExecutedMigrations(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to get executed migrations: %w", err)
	}

	// 4. Выполняем новые миграции
	for _, filename := range migrationFiles {
		if executed[filename] {
			continue
		}

		if err := m.runMigration(ctx, conn, migrationsDir, filename); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", filename, err)
		}
		fmt.Printf("Migration applied: %s\n", filename)
	}

	return nil
}

func (m *MigrationRunner) ensureMigrationsTable(ctx context.Context, q migrationQuerier) error {
	schemaName := quoteIdentifier(m.schema)
	query := fmt.Sprintf(`
		CREATE SCHEMA IF NOT EXISTS %s;
		CREATE TABLE IF NOT EXISTS %s.schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`, schemaName, schemaName)
	_, err := q.ExecContext(ctx, query)
	return err
}

func (m *MigrationRunner) getExecutedMigrations(ctx context.Context, q migrationQuerier) (map[string]bool, error) {
	query := fmt.Sprintf("SELECT version FROM %s.schema_migrations", quoteIdentifier(m.schema))
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		res[v] = true
	}
	return res, nil
}

func (m *MigrationRunner) runMigration(ctx context.Context, conn migrationConnection, dir, filename string) error {
	path := filepath.Join(dir, filename)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL search_path TO %s", quoteIdentifier(m.schema))); err != nil {
		return fmt.Errorf("failed to set migration search_path: %w", err)
	}

	// Выполняем SQL из файла. Дополнительно поддерживаем ограниченный \copy
	// для загрузки локальных TSV/CSV seed-файлов из директории миграций.
	if err := m.execMigrationContent(ctx, tx, dir, string(content)); err != nil {
		return fmt.Errorf("sql execution error: %w", err)
	}

	// Записываем информацию о миграции
	query := fmt.Sprintf("INSERT INTO %s.schema_migrations (version) VALUES ($1)", quoteIdentifier(m.schema))
	if _, err := tx.ExecContext(ctx, query, filename); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

func (m *MigrationRunner) execMigrationContent(ctx context.Context, tx *sql.Tx, dir string, content string) error {
	var sqlPart strings.Builder
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `\copy `) {
			if err := execSQLPart(ctx, tx, sqlPart.String()); err != nil {
				return err
			}
			sqlPart.Reset()
			if err := m.execCopyDirective(ctx, tx, dir, strings.TrimSpace(line)); err != nil {
				return err
			}
			continue
		}
		sqlPart.WriteString(line)
		sqlPart.WriteByte('\n')
	}
	return execSQLPart(ctx, tx, sqlPart.String())
}

func execSQLPart(ctx context.Context, tx *sql.Tx, sqlPart string) error {
	if strings.TrimSpace(sqlPart) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, sqlPart)
	return err
}

var copyDirectiveRE = regexp.MustCompile(`^\\copy\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s+FROM\s+'([^']+)'\s+WITH\s*\(([^)]*)\)\s*;?$`)

func (m *MigrationRunner) execCopyDirective(ctx context.Context, tx *sql.Tx, dir string, directive string) error {
	matches := copyDirectiveRE.FindStringSubmatch(directive)
	if matches == nil {
		return fmt.Errorf("unsupported copy directive: %s", directive)
	}

	table := matches[1]
	columns := splitCSVNames(matches[2])
	options := strings.ToLower(matches[4])
	delimiter := ','
	if strings.Contains(options, `delimiter e'\t'`) || strings.Contains(options, `delimiter '\t'`) || strings.Contains(options, "delimiter tab") {
		delimiter = '\t'
	}
	hasHeader := strings.Contains(options, "header true") || strings.Contains(options, "header")

	sourcePath := matches[3]
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(dir, sourcePath)
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = delimiter
	reader.FieldsPerRecord = len(columns)
	reader.TrimLeadingSpace = false
	if hasHeader {
		if _, err := reader.Read(); err != nil {
			return fmt.Errorf("failed to read copy header from %s: %w", sourcePath, err)
		}
	}

	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema(m.schema, table, columns...))
	if err != nil {
		return err
	}
	defer stmt.Close()

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		values := make([]any, len(record))
		for i, value := range record {
			if value == `\N` {
				values[i] = nil
			} else {
				values[i] = value
			}
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return err
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return err
	}
	return nil
}

func splitCSVNames(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			result = append(result, name)
		}
	}
	return result
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
