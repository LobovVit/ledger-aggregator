package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ledger-aggregator/backend/internal/api"
	"ledger-aggregator/backend/internal/config"
	"ledger-aggregator/backend/internal/model"
	"ledger-aggregator/backend/internal/repository"
	"ledger-aggregator/backend/internal/service"
	"ledger-aggregator/backend/internal/svap"

	_ "github.com/lib/pq"
)

func main() {
	// 0. Подключение к БД
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		getEnv("DB_NAME", "ledger_aggregator"),
		getEnv("DB_SSLMODE", "disable"),
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping db: %v", err)
	}

	// 0.1 Выполнение миграций
	migrationRunner := repository.NewMigrationRunner(db)
	if err := migrationRunner.Run(context.Background(), "db/migrations"); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// 1. Конфигурация
	configRepo := repository.NewPostgresConfigRepository(db)
	configService := config.NewConfigService(configRepo)
	cfg := configService.GetCurrent()

	// 2. Инициализация зависимостей
	svapClient := &svap.MockClient{} // В реальности svap.NewRealClient(configService)

	attrRepo := &mockAttrRepo{}
	queryRepo := &mockQueryRepo{}
	resultRepo := &mockResultRepo{}
	dictRepo := &mockDictRepo{}

	aggregator := service.NewAggregatorService(svapClient, attrRepo, queryRepo, resultRepo, dictRepo)

	// 3. Запуск фоновых задач
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aggregator.StartSyncJob(ctx, 24*time.Hour)

	// Запуск слушателя обновлений конфигурации (для многоузловой работы)
	go listenConfigUpdates(ctx, configService)

	// 4. Настройка HTTP сервера
	handler := api.NewHandler(aggregator, configService)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: mux,
	}

	// 5. Грациозное завершение
	go func() {
		fmt.Printf("Starting server on port %s...\n", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("Shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	server.Shutdown(shutdownCtx)
}

// Заглушки для компиляции
type mockAttrRepo struct{}

func (m *mockAttrRepo) SaveAll(ctx context.Context, attrs []model.AnalyticalAttribute) error {
	return nil
}
func (m *mockAttrRepo) GetAll(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	return nil, nil
}
func (m *mockAttrRepo) GetByBusiness(ctx context.Context, business string) ([]model.AnalyticalAttribute, error) {
	return nil, nil
}

type mockQueryRepo struct{}

func (m *mockQueryRepo) Save(ctx context.Context, q model.SavedQuery) error { return nil }
func (m *mockQueryRepo) GetByID(ctx context.Context, id string) (model.SavedQuery, error) {
	return model.SavedQuery{}, nil
}
func (m *mockQueryRepo) GetByUserID(ctx context.Context, userID string) ([]model.SavedQuery, error) {
	return nil, nil
}
func (m *mockQueryRepo) Update(ctx context.Context, q model.SavedQuery) error { return nil }

type mockResultRepo struct{}

func (m *mockResultRepo) Save(ctx context.Context, res model.QueryResult, rows []model.QueryResultRow, values []model.QueryResultValue) error {
	return nil
}
func (m *mockResultRepo) GetByQueryID(ctx context.Context, queryID string) ([]model.QueryResult, error) {
	return nil, nil
}
func (m *mockResultRepo) GetByID(ctx context.Context, id string) (model.QueryResult, error) {
	return model.QueryResult{}, nil
}
func (m *mockResultRepo) GetRows(ctx context.Context, resultID string) ([]model.QueryResultRow, error) {
	return nil, nil
}
func (m *mockResultRepo) GetValuesByRowID(ctx context.Context, rowID string) ([]model.QueryResultValue, error) {
	return nil, nil
}
func (m *mockResultRepo) GetFullResultData(ctx context.Context, resultID string) ([]map[string]any, error) {
	return nil, nil
}

type mockDictRepo struct{}

func (m *mockDictRepo) Get(ctx context.Context, business string, dictionaryCode string, key string) (string, error) {
	return "", nil
}
func (m *mockDictRepo) Set(ctx context.Context, business string, dictionaryCode string, key string, value string) error {
	return nil
}
func (m *mockDictRepo) Search(ctx context.Context, business string, dictionaryCode string, query string) ([]model.DictionaryItem, error) {
	return nil, nil
}

type mockConfigRepo struct{}

func (m *mockConfigRepo) Save(ctx context.Context, groupName string, cfg any) error { return nil }
func (m *mockConfigRepo) Load(ctx context.Context, groupName string) (any, error) {
	switch groupName {
	case config.GroupServer:
		return config.ServerConfig{Port: "8080"}, nil
	case config.GroupSVAP:
		return config.SVAPConfig{
			GKHost: "http://svap-gk",
			LSHost: "http://svap-ls",
			JOHost: "http://svap-jo",
		}, nil
	case config.GroupRetention:
		return config.RetentionConfig{
			DefaultTTL: "24h",
			RoleTTLs:   map[string]string{"admin": "72h"},
			OrgTTLs:    make(map[string]string),
			UserTTLs:   make(map[string]string),
		}, nil
	}
	return nil, nil
}
func (m *mockConfigRepo) LoadAll(ctx context.Context) (map[string]any, error) {
	return map[string]any{
		config.GroupServer: config.ServerConfig{Port: "8080"},
		config.GroupSVAP: config.SVAPConfig{
			GKHost: "http://svap-gk",
			LSHost: "http://svap-ls",
			JOHost: "http://svap-jo",
		},
		config.GroupRetention: config.RetentionConfig{
			DefaultTTL: "24h",
			RoleTTLs:   map[string]string{"admin": "72h"},
			OrgTTLs:    make(map[string]string),
			UserTTLs:   make(map[string]string),
		},
	}, nil
}

func listenConfigUpdates(ctx context.Context, s *config.ConfigService) {
	// В реальности здесь используется PQ Listener для Postgres
	// Для демонстрации гарантированной доставки добавим логику переподключения
	for {
		log.Println("Config update listener connecting...")
		// 1. Устанавливаем LISTEN
		// 2. В цикле ждем уведомлений
		// 3. Если получили уведомление с payload (имя группы):
		//    s.ReloadGroup(ctx, payload)
		// 4. Если ошибка соединения - делаем Backoff и RECONNECT
		// 5. ПОСЛЕ восстановления связи ВСЕГДА вызываем s.ReloadAll(ctx) для синхронизации пропущенного

		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute): // Имитация периодической сверки (Polling)
			_ = s.ReloadAll(ctx)
		}
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
