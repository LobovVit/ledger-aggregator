package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"reflect"
	"sync"
	"time"

	"svap-query-service/backend/internal/repository"
)

// Константы для ключей групп конфигурации
const (
	GroupServer    = "server"
	GroupSVAP      = "svap"
	GroupRetention = "retention"
)

type RetentionConfig struct {
	DefaultTTL string            `json:"default_ttl"` // Например, "24h"
	RoleTTLs   map[string]string `json:"role_ttls"`   // Напр. {"admin": "72h"}
	OrgTTLs    map[string]string `json:"org_ttls"`    // Напр. {"9500": "288h"}
	UserTTLs   map[string]string `json:"user_ttls"`   // Напр. {"lobov.vit": "144h"}
}

type SVAPEndpoint struct {
	Host   string `json:"host"`
	Suffix string `json:"suffix"`
}

type SVAPConfig struct {
	Endpoints map[string]SVAPEndpoint `json:"endpoints"` // Ключ - тип запроса (FSG, TURN, PA, CONS и т.д.)
}

type ServerConfig struct {
	Port string `json:"port"`
}

type AppConfig struct {
	Server    ServerConfig    `json:"server"`
	SVAP      SVAPConfig      `json:"svap"`
	Retention RetentionConfig `json:"retention"`
}

// ConfigService управляет конфигурацией приложения, разделенной на группы
type ConfigService struct {
	mu      sync.RWMutex
	current *AppConfig
	pending *AppConfig
	repo    repository.ConfigRepository
}

func NewConfigService(repo repository.ConfigRepository) *ConfigService {
	// Дефолтные значения из переменных окружения
	initial := &AppConfig{
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		SVAP: SVAPConfig{
			Endpoints: map[string]SVAPEndpoint{
				"FSG": {
					Host:   getEnv("SVAP_FSG_HOST", os.Getenv("SVAP_HOST_GK")),
					Suffix: getEnv("SVAP_FSG_SUFFIX", "/api/query/execute"),
				},
				"TURN": {
					Host:   getEnv("SVAP_TURN_HOST", os.Getenv("SVAP_HOST_GK")),
					Suffix: getEnv("SVAP_TURN_SUFFIX", "/api/query/execute"),
				},
				"PA": {
					Host:   getEnv("SVAP_PA_HOST", os.Getenv("SVAP_HOST_GK")),
					Suffix: getEnv("SVAP_PA_SUFFIX", "/api/query/execute"),
				},
				"CONS": {
					Host:   getEnv("SVAP_CONS_HOST", os.Getenv("SVAP_HOST_GK")),
					Suffix: getEnv("SVAP_CONS_SUFFIX", "/api/query/execute"),
				},
			},
		},
		Retention: RetentionConfig{
			DefaultTTL: "24h",
			RoleTTLs: map[string]string{
				"admin": "72h",
			},
			OrgTTLs:  make(map[string]string),
			UserTTLs: make(map[string]string),
		},
	}

	s := &ConfigService{
		current: initial,
		pending: cloneAppConfig(initial),
		repo:    repo,
	}

	// Загружаем всё из БД при старте
	if err := s.ReloadAll(context.Background()); err != nil {
		log.Printf("Warning: failed to load config from DB on startup: %v", err)
	}

	return s
}

func (s *ConfigService) GetCurrent() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *cloneAppConfig(s.current)
}

func (s *ConfigService) GetPending() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *cloneAppConfig(s.pending)
}

func (s *ConfigService) UpdatePending(newCfg AppConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = cloneAppConfig(&newCfg)
}

// UpdatePendingFromRaw выполняет частичное обновление подготовленной конфигурации из JSON
func (s *ConfigService) UpdatePendingFromRaw(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Используем временную карту для определения того, какие группы были переданы в JSON
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	if serverData, ok := rawMap[GroupServer]; ok {
		if err := json.Unmarshal(serverData, &s.pending.Server); err != nil {
			return fmt.Errorf("failed to unmarshal server config: %w", err)
		}
	}

	if svapData, ok := rawMap[GroupSVAP]; ok {
		if err := json.Unmarshal(svapData, &s.pending.SVAP); err != nil {
			return fmt.Errorf("failed to unmarshal svap config: %w", err)
		}
		if s.pending.SVAP.Endpoints == nil {
			s.pending.SVAP.Endpoints = make(map[string]SVAPEndpoint)
		}
	}

	if retentionData, ok := rawMap[GroupRetention]; ok {
		if err := json.Unmarshal(retentionData, &s.pending.Retention); err != nil {
			return fmt.Errorf("failed to unmarshal retention config: %w", err)
		}
		normalizeRetentionConfig(&s.pending.Retention)
	}

	return nil
}

// Apply сохраняет текущую подготовленную конфигурацию в БД по группам
func (s *ConfigService) Apply(ctx context.Context) error {
	s.mu.Lock()
	pending := *cloneAppConfig(s.pending)
	current := *cloneAppConfig(s.current)
	s.mu.Unlock()

	if s.repo == nil {
		s.mu.Lock()
		s.current = cloneAppConfig(&pending)
		s.pending = cloneAppConfig(&pending)
		s.mu.Unlock()
		return nil
	}

	// Сохраняем группу Server, если изменилась
	if pending.Server != current.Server {
		if err := s.repo.Save(ctx, GroupServer, pending.Server); err != nil {
			return fmt.Errorf("failed to save server config: %w", err)
		}
	}

	// Сохраняем группу SVAP
	if err := s.repo.Save(ctx, GroupSVAP, pending.SVAP); err != nil {
		return fmt.Errorf("failed to save svap config: %w", err)
	}

	// Сохраняем группу Retention, если изменилась
	if !reflect.DeepEqual(pending.Retention, current.Retention) {
		if err := s.repo.Save(ctx, GroupRetention, pending.Retention); err != nil {
			return fmt.Errorf("failed to save retention config: %w", err)
		}
	}

	s.mu.Lock()
	s.current = cloneAppConfig(&pending)
	s.pending = cloneAppConfig(&pending)
	s.mu.Unlock()

	return nil
}

// ReloadGroup обновляет конкретную группу из БД
func (s *ConfigService) ReloadGroup(ctx context.Context, group string) error {
	if s.repo == nil {
		return nil
	}

	data, err := s.repo.Load(ctx, group)
	if err != nil {
		return err
	}
	if data == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch group {
	case GroupServer:
		if cfg, ok := data.(ServerConfig); ok {
			s.current.Server = cfg
			s.pending.Server = cfg
		} else {
			// Если пришел map (из JSON в mock/db)
			if err := mapToStruct(data, &s.current.Server); err == nil {
				s.pending.Server = s.current.Server
			}
		}
	case GroupSVAP:
		if cfg, ok := data.(SVAPConfig); ok {
			if cfg.Endpoints == nil {
				cfg.Endpoints = make(map[string]SVAPEndpoint)
			}
			s.current.SVAP = cfg
			s.pending.SVAP = cfg
		} else {
			if err := mapToStruct(data, &s.current.SVAP); err == nil {
				if s.current.SVAP.Endpoints == nil {
					s.current.SVAP.Endpoints = make(map[string]SVAPEndpoint)
				}
				s.pending.SVAP = s.current.SVAP
			}
		}
	case GroupRetention:
		if cfg, ok := data.(RetentionConfig); ok {
			normalizeRetentionConfig(&cfg)
			s.current.Retention = cfg
			s.pending.Retention = cfg
		} else {
			if err := mapToStruct(data, &s.current.Retention); err == nil {
				normalizeRetentionConfig(&s.current.Retention)
				s.pending.Retention = s.current.Retention
			}
		}
	}

	log.Printf("Configuration group '%s' reloaded", group)
	return nil
}

// ReloadAll загружает все группы из БД
func (s *ConfigService) ReloadAll(ctx context.Context) error {
	return s.reloadAllInternal(ctx, true)
}

func (s *ConfigService) reloadAllInternal(ctx context.Context, updateCurrent bool) error {
	if s.repo == nil {
		return nil
	}

	all, err := s.repo.LoadAll(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if val, ok := all[GroupServer]; ok {
		if err := mapToStruct(val, &s.current.Server); err != nil {
			return fmt.Errorf("failed to decode server config: %w", err)
		}
		s.pending.Server = s.current.Server
	}
	if val, ok := all[GroupSVAP]; ok {
		if err := mapToStruct(val, &s.current.SVAP); err != nil {
			return fmt.Errorf("failed to decode svap config: %w", err)
		}
		if s.current.SVAP.Endpoints == nil {
			s.current.SVAP.Endpoints = make(map[string]SVAPEndpoint)
		}
		s.pending.SVAP = s.current.SVAP
	}
	if val, ok := all[GroupRetention]; ok {
		if err := mapToStruct(val, &s.current.Retention); err != nil {
			return fmt.Errorf("failed to decode retention config: %w", err)
		}
		normalizeRetentionConfig(&s.current.Retention)
		s.pending.Retention = s.current.Retention
	}
	s.pending = cloneAppConfig(s.current)

	log.Println("All configuration groups reloaded from DB")
	return nil
}

// GetRetentionTTL возвращает время жизни витрины исходя из иерархии в порядке возрастания приоритета:
// default_ttl -> org_ttls -> role_ttls -> user_ttls
func (s *ConfigService) GetRetentionTTL(userID string, roles []string, orgCode string) time.Duration {
	s.mu.RLock()
	cfg := s.current.Retention
	s.mu.RUnlock()

	var resultTTL time.Duration

	// 1. Значение по умолчанию
	if d, err := time.ParseDuration(cfg.DefaultTTL); err == nil {
		resultTTL = d
	} else {
		resultTTL = 24 * time.Hour // Запасной дефолт
	}

	// 2. Приоритет организации
	if orgCode != "" {
		if oTTLStr, ok := cfg.OrgTTLs[orgCode]; ok {
			if d, err := time.ParseDuration(oTTLStr); err == nil {
				resultTTL = d
			}
		}
	}

	// 3. Приоритет ролей (выбираем максимальный из ролей пользователя)
	var maxRoleTTL time.Duration
	foundRoleTTL := false
	for _, role := range roles {
		if rTTLStr, ok := cfg.RoleTTLs[role]; ok {
			if d, err := time.ParseDuration(rTTLStr); err == nil {
				if !foundRoleTTL || d > maxRoleTTL {
					maxRoleTTL = d
					foundRoleTTL = true
				}
			}
		}
	}
	if foundRoleTTL {
		resultTTL = maxRoleTTL
	}

	// 4. Приоритет пользователя
	if userID != "" {
		if uTTLStr, ok := cfg.UserTTLs[userID]; ok {
			if d, err := time.ParseDuration(uTTLStr); err == nil {
				resultTTL = d
			}
		}
	}

	return resultTTL
}

func cloneAppConfig(src *AppConfig) *AppConfig {
	if src == nil {
		return &AppConfig{}
	}

	dst := *src
	dst.SVAP.Endpoints = make(map[string]SVAPEndpoint, len(src.SVAP.Endpoints))
	for k, v := range src.SVAP.Endpoints {
		dst.SVAP.Endpoints[k] = v
	}

	dst.Retention.RoleTTLs = cloneStringMap(src.Retention.RoleTTLs)
	dst.Retention.OrgTTLs = cloneStringMap(src.Retention.OrgTTLs)
	dst.Retention.UserTTLs = cloneStringMap(src.Retention.UserTTLs)
	normalizeRetentionConfig(&dst.Retention)

	return &dst
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func normalizeRetentionConfig(cfg *RetentionConfig) {
	if cfg.DefaultTTL == "" {
		cfg.DefaultTTL = "24h"
	}
	if cfg.RoleTTLs == nil {
		cfg.RoleTTLs = make(map[string]string)
	}
	if cfg.OrgTTLs == nil {
		cfg.OrgTTLs = make(map[string]string)
	}
	if cfg.UserTTLs == nil {
		cfg.UserTTLs = make(map[string]string)
	}
}

func mapToStruct(src any, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
