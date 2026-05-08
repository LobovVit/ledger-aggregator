package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"ledger-aggregator/backend/internal/repository"
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

type SVAPConfig struct {
	GKHost string `json:"gk_host"`
	LSHost string `json:"ls_host"`
	JOHost string `json:"jo_host"`
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
			GKHost: os.Getenv("SVAP_HOST_GK"),
			LSHost: os.Getenv("SVAP_HOST_LS"),
			JOHost: os.Getenv("SVAP_HOST_JO"),
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
		pending: &AppConfig{
			Server: initial.Server,
			SVAP:   initial.SVAP,
		},
		repo: repo,
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
	return *s.current
}

func (s *ConfigService) GetPending() AppConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.pending
}

func (s *ConfigService) UpdatePending(newCfg AppConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = &newCfg
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
	}

	if retentionData, ok := rawMap[GroupRetention]; ok {
		if err := json.Unmarshal(retentionData, &s.pending.Retention); err != nil {
			return fmt.Errorf("failed to unmarshal retention config: %w", err)
		}
	}

	return nil
}

// Apply сохраняет текущую подготовленную конфигурацию в БД по группам
func (s *ConfigService) Apply(ctx context.Context) error {
	s.mu.Lock()
	pending := *s.pending
	current := *s.current
	s.mu.Unlock()

	if s.repo == nil {
		s.mu.Lock()
		s.current = &pending
		s.mu.Unlock()
		return nil
	}

	// Сохраняем группу Server, если изменилась
	if pending.Server != current.Server {
		if err := s.repo.Save(ctx, GroupServer, pending.Server); err != nil {
			return fmt.Errorf("failed to save server config: %w", err)
		}
	}

	// Сохраняем группу SVAP, если изменилась
	if pending.SVAP != current.SVAP {
		if err := s.repo.Save(ctx, GroupSVAP, pending.SVAP); err != nil {
			return fmt.Errorf("failed to save svap config: %w", err)
		}
	}

	// Сохраняем группу Retention, если изменилась
	if pending.Retention.DefaultTTL != current.Retention.DefaultTTL ||
		fmt.Sprintf("%v", pending.Retention.RoleTTLs) != fmt.Sprintf("%v", current.Retention.RoleTTLs) ||
		fmt.Sprintf("%v", pending.Retention.OrgTTLs) != fmt.Sprintf("%v", current.Retention.OrgTTLs) ||
		fmt.Sprintf("%v", pending.Retention.UserTTLs) != fmt.Sprintf("%v", current.Retention.UserTTLs) {
		if err := s.repo.Save(ctx, GroupRetention, pending.Retention); err != nil {
			return fmt.Errorf("failed to save retention config: %w", err)
		}
	}

	s.mu.Lock()
	s.current = &pending
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
			s.current.SVAP = cfg
			s.pending.SVAP = cfg
		} else {
			if err := mapToStruct(data, &s.current.SVAP); err == nil {
				s.pending.SVAP = s.current.SVAP
			}
		}
	case GroupRetention:
		if cfg, ok := data.(RetentionConfig); ok {
			s.current.Retention = cfg
			s.pending.Retention = cfg
		} else {
			if err := mapToStruct(data, &s.current.Retention); err == nil {
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
		_ = mapToStruct(val, &s.current.Server)
		s.pending.Server = s.current.Server
	}
	if val, ok := all[GroupSVAP]; ok {
		_ = mapToStruct(val, &s.current.SVAP)
		s.pending.SVAP = s.current.SVAP
	}
	if val, ok := all[GroupRetention]; ok {
		_ = mapToStruct(val, &s.current.Retention)
		s.pending.Retention = s.current.Retention
	}

	log.Println("All configuration groups reloaded from DB")
	return nil
}

// GetRetentionTTL возвращает время жизни витрины исходя из иерархии: пользователь -> роль -> организация -> значение по умолчанию
func (s *ConfigService) GetRetentionTTL(userID string, roles []string, orgCode string) time.Duration {
	s.mu.RLock()
	cfg := s.current.Retention
	s.mu.RUnlock()

	// 1. Приоритет пользователя
	if uTTLStr, ok := cfg.UserTTLs[userID]; ok {
		if d, err := time.ParseDuration(uTTLStr); err == nil {
			return d
		}
	}

	// 2. Приоритет ролей (выбираем максимальный из ролей пользователя)
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
		return maxRoleTTL
	}

	// 3. Приоритет организации
	if oTTLStr, ok := cfg.OrgTTLs[orgCode]; ok {
		if d, err := time.ParseDuration(oTTLStr); err == nil {
			return d
		}
	}

	// 4. Значение по умолчанию
	d, err := time.ParseDuration(cfg.DefaultTTL)
	if err != nil {
		return 24 * time.Hour // Запасной дефолт
	}
	return d
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
