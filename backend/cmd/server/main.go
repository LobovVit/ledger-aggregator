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

	"svap-query-service/backend/internal/api"
	"svap-query-service/backend/internal/config"
	"svap-query-service/backend/internal/repository"
	"svap-query-service/backend/internal/service"
	"svap-query-service/backend/internal/svap"

	_ "svap-query-service/backend/docs"

	"github.com/lib/pq"
	"github.com/swaggo/http-swagger/v2"
)

// @title API SVAP Query Service / API агрегатора витрин
// @version 1.0
// @description RU: API сервиса SVAP Query Service для интеграции со СВАП, управления сохраненными запросами, результатами и динамической конфигурацией. EN: API server for SVAP Query Service, used to integrate with SVAP and manage saved queries, query results, and dynamic configuration.

// @host localhost:8080
// @BasePath /api/v1

func main() {
	// 0. Подключение к БД
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		getEnv("DB_HOST", "localhost"),
		getEnv("DB_PORT", "5432"),
		getEnv("DB_USER", "postgres"),
		getEnv("DB_PASSWORD", "postgres"),
		getEnv("DB_NAME", "svap_query_service"),
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
	migrationsDir := getEnv("MIGRATIONS_DIR", "db/migrations")
	if err := migrationRunner.Run(context.Background(), migrationsDir); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// 1. Конфигурация
	configRepo := repository.NewPostgresConfigRepository(db)
	configService := config.NewConfigService(configRepo)
	cfg := configService.GetCurrent()

	// 2. Инициализация зависимостей
	svapClient := svap.NewRealClient(configService)

	attrRepo := repository.NewPostgresAnalyticalAttributeRepository(db)
	queryRepo := repository.NewPostgresSavedQueryRepository(db)
	resultRepo := repository.NewPostgresQueryResultRepository(db)
	dictRepo := repository.NewPostgresDictionaryCacheRepository(db)

	aggregator := service.NewAggregatorService(svapClient, attrRepo, queryRepo, resultRepo, dictRepo, configService)

	// 3. Запуск фоновых задач
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	aggregator.StartSyncJob(ctx, 24*time.Hour)

	// Запуск слушателя обновлений конфигурации (для многоузловой работы)
	go listenConfigUpdates(ctx, dsn, configService)

	// 4. Настройка HTTP сервера
	handler := api.NewHandler(aggregator, configService)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	mux.Handle("GET /swagger/", httpSwagger.WrapHandler)

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

type mockConfigRepo struct{}

func (m *mockConfigRepo) Save(ctx context.Context, groupName string, cfg any) error { return nil }
func (m *mockConfigRepo) Load(ctx context.Context, groupName string) (any, error) {
	switch groupName {
	case config.GroupServer:
		return config.ServerConfig{Port: "8080"}, nil
	case config.GroupSVAP:
		return config.SVAPConfig{
			Endpoints: map[string]config.SVAPEndpoint{
				"FSG": {Host: "http://svap-gk", Suffix: "/api/query/execute"},
			},
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
			Endpoints: map[string]config.SVAPEndpoint{
				"FSG": {Host: "http://svap-gk", Suffix: "/api/query/execute"},
			},
		},
		config.GroupRetention: config.RetentionConfig{
			DefaultTTL: "24h",
			RoleTTLs:   map[string]string{"admin": "72h"},
			OrgTTLs:    make(map[string]string),
			UserTTLs:   make(map[string]string),
		},
	}, nil
}

func listenConfigUpdates(ctx context.Context, dsn string, s *config.ConfigService) {
	listener := pq.NewListener(dsn, 5*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("config listener event %v: %v", ev, err)
		}
	})
	defer listener.Close()

	if err := listener.Listen("config_updated"); err != nil {
		log.Printf("failed to listen for config updates: %v", err)
	}

	pollTicker := time.NewTicker(10 * time.Minute)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-listener.Notify:
			if notification == nil {
				if err := s.ReloadAll(ctx); err != nil {
					log.Printf("failed to reload all config after listener reconnect: %v", err)
				}
				continue
			}
			if notification.Extra == "" {
				if err := s.ReloadAll(ctx); err != nil {
					log.Printf("failed to reload all config: %v", err)
				}
				continue
			}
			if err := s.ReloadGroup(ctx, notification.Extra); err != nil {
				log.Printf("failed to reload config group %q: %v", notification.Extra, err)
			}
		case <-pollTicker.C:
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
