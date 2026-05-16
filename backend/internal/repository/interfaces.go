package repository

import "svap-query-service/backend/internal/ports"

// These aliases keep old imports working while the application ports live in
// internal/ports. Concrete Postgres adapters in this package implement them
// implicitly.
type AnalyticalAttributeRepository = ports.AnalyticalAttributeRepository
type DictionaryCacheRepository = ports.DictionaryCacheRepository
type SavedQueryRepository = ports.SavedQueryRepository
type QueryResultRepository = ports.QueryResultRepository
type QueryExecutionRepository = ports.QueryExecutionRepository
type ConfigRepository = ports.ConfigRepository
