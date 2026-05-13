package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"svap-query-service/backend/internal/config"
	"svap-query-service/backend/internal/model"
	"svap-query-service/backend/internal/repository"
	"svap-query-service/backend/internal/service"
	"svap-query-service/backend/internal/svap"

	"github.com/google/uuid"

	_ "github.com/lib/pq"
)

func main() {
	// 1. Setup DB
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

	// 2. Setup dependencies
	configRepo := repository.NewPostgresConfigRepository(db)
	configService := config.NewConfigService(configRepo)

	// Set real SVAP host for testing
	cfg := configService.GetCurrent()
	cfg.SVAP.Endpoints["FSG"] = config.SVAPEndpoint{
		Host:   "https://fk-eb-svap-dev-fb-svip-oltp1.otr.ru",
		Suffix: "/api/query/execute",
	}
	configService.UpdatePending(cfg)
	configService.Apply(context.Background())

	svapClient := svap.NewRealClient(configService)
	attrRepo := repository.NewPostgresAnalyticalAttributeRepository(db)
	queryRepo := repository.NewPostgresSavedQueryRepository(db)
	resultRepo := repository.NewPostgresQueryResultRepository(db)
	dictRepo := repository.NewPostgresDictionaryCacheRepository(db)

	aggregator := service.NewAggregatorService(svapClient, attrRepo, queryRepo, resultRepo, dictRepo, configService)

	ctx := context.Background()

	// 3. Create a test query
	testQuery := model.SavedQuery{
		ID:          uuid.NewString(),
		UserID:      "test-user",
		Name:        "Test FSG Query",
		Description: "Integration test query",
		Visibility:  model.VisibilityPublic,
		QueryType:   "FSG",
		Params: `{
			"DateMode": 0,
			"BeginBalance": 0,
			"VisualParams": ["kbk", "targetCode", "oktmo", "pa", "account"],
			"FilterParams": [
				{"operation": "=", "value": "99010001", "param": "budgetCode"},
				{"operation": "=", "value": "-", "param": "pzo"},
				{"param": "pzo", "operation": "empty"},
				{"param": "pzo", "operation": "not exists"}
			],
			"Book": "FTOperFed",
			"Mode": 0,
			"RecType": 1,
			"StartRepDate": "2026-01-01",
			"EndRepDate": "2026-04-26",
			"NullStr": 0,
			"ReturnIfEmpty": 1
		}`,
		CreatedAt: time.Now(),
	}

	fmt.Println("Saving test query...")
	testQueryID, err := queryRepo.Save(ctx, testQuery)
	if err != nil {
		fmt.Printf("Failed to save query: %v\n", err)
		return
	}
	testQuery.ID = testQueryID

	// 4. Execute query
	fmt.Println("Executing and saving query result...")
	result, err := aggregator.ExecuteAndSaveQuery(ctx, testQuery.ID, 0, 10, "", "", "Overridden Name", "Custom Desc", model.VisibilityPrivate, []string{"admin"}, "ORG001")
	if err != nil {
		fmt.Printf("Execution failed (expected in some envs): %v\n", err)
	} else {
		fmt.Printf("Success! Result ID: %s, Name: %s, Visibility: %s, ExpiresAt: %v\n", result.ID, result.Name, result.Visibility, result.ExpiresAt)
	}

	// 5. Verify data in DB
	queries, err := aggregator.GetSavedQueries(ctx, "test-user")
	if err != nil {
		fmt.Printf("Failed to fetch queries from DB: %v\n", err)
		return
	}
	fmt.Printf("Found %d saved queries for test-user.\n", len(queries))
	for _, q := range queries {
		fmt.Printf(" - ID: %s, CreatedAt: %s\n", q.ID, q.CreatedAt)
	}

	if err == nil && result.ID != "" {
		data, err := resultRepo.GetFullResultData(ctx, result.ID, 0, 0)
		if err != nil {
			fmt.Printf("Failed to fetch result data from DB: %v\n", err)
			return
		}

		fmt.Printf("Fetched %d rows from DB.\n", len(data))
		if len(data) > 0 {
			fmt.Printf("First row: %+v\n", data[0])
		}

		// 6. Test deletions
		fmt.Printf("Deleting result %s...\n", result.ID)
		if err := aggregator.DeleteQueryResult(ctx, result.ID); err != nil {
			fmt.Printf("Failed to delete result: %v\n", err)
		} else {
			fmt.Println("Result deleted successfully.")
		}
	}

	fmt.Printf("Deleting query %s...\n", testQuery.ID)
	if err := aggregator.DeleteSavedQuery(ctx, testQuery.ID); err != nil {
		fmt.Printf("Failed to delete query: %v\n", err)
	} else {
		fmt.Println("Query deleted successfully.")
	}

	// 7. Verify deletion
	allResults, _ := aggregator.GetQueryResults(ctx, "")
	fmt.Printf("Total results in DB after deletion: %d\n", len(allResults))
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
