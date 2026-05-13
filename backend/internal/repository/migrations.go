package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrationRunner отвечает за выполнение SQL миграций
type MigrationRunner struct {
	db *sql.DB
}

func NewMigrationRunner(db *sql.DB) *MigrationRunner {
	return &MigrationRunner{db: db}
}

// Run выполняет все новые миграции из указанной директории.
// Использует PostgreSQL Advisory Lock для предотвращения одновременного запуска несколькими узлами.
func (m *MigrationRunner) Run(ctx context.Context, migrationsDir string) error {
	// Блокировка на уровне БД, чтобы только один экземпляр выполнял миграции одновременно.
	// Используем произвольное число (например, 12345) в качестве ID лока.
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to reserve db connection for advisory lock: %w", err)
	}
	defer conn.Close()

	lockID := 12345
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID)
	}()

	// 1. Создаем таблицу миграций, если её нет
	if err := m.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	// ФИКС: Если мы используем PostgreSQL 17+, gen_random_uuid() доступен из коробки.
	// Но для старых версий или на всякий случай, убедимся что pgcrypto доступен.
	if _, err := m.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto;"); err != nil {
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
	executed, err := m.getExecutedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get executed migrations: %w", err)
	}

	// 4. Выполняем новые миграции
	for _, filename := range migrationFiles {
		if executed[filename] {
			continue
		}

		if err := m.runMigration(ctx, migrationsDir, filename); err != nil {
			return fmt.Errorf("failed to run migration %s: %w", filename, err)
		}
		fmt.Printf("Migration applied: %s\n", filename)
	}

	return nil
}

func (m *MigrationRunner) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := m.db.ExecContext(ctx, query)
	return err
}

func (m *MigrationRunner) getExecutedMigrations(ctx context.Context) (map[string]bool, error) {
	rows, err := m.db.QueryContext(ctx, "SELECT version FROM schema_migrations")
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

func (m *MigrationRunner) runMigration(ctx context.Context, dir, filename string) error {
	path := filepath.Join(dir, filename)
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Выполняем SQL из файла
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("sql execution error: %w", err)
	}

	// Записываем информацию о миграции
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", filename); err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}
