package config

import (
	"context"
	"testing"
	"time"
)

type fakeConfigRepo struct {
	saved map[string]any
}

func (r *fakeConfigRepo) Save(ctx context.Context, groupName string, cfg any) error {
	if r.saved == nil {
		r.saved = make(map[string]any)
	}
	r.saved[groupName] = cfg
	return nil
}

func (r *fakeConfigRepo) Load(ctx context.Context, groupName string) (any, error) {
	return nil, nil
}

func (r *fakeConfigRepo) LoadAll(ctx context.Context) (map[string]any, error) {
	return map[string]any{}, nil
}

func TestApplyPreservesDefaultRetention(t *testing.T) {
	repo := &fakeConfigRepo{}
	service := NewConfigService(repo)

	pending := service.GetPending()
	pending.Server.Port = "9090"
	service.UpdatePending(pending)

	if err := service.Apply(context.Background()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got := service.GetCurrent().Retention
	if got.DefaultTTL != "24h" {
		t.Fatalf("DefaultTTL = %q, want %q", got.DefaultTTL, "24h")
	}
	if got.RoleTTLs == nil || got.OrgTTLs == nil || got.UserTTLs == nil {
		t.Fatalf("retention maps must be initialized: %#v", got)
	}
}

func TestGetRetentionTTLHierarchy(t *testing.T) {
	service := NewConfigService(nil)
	service.UpdatePending(AppConfig{
		Server: ServerConfig{Port: "8080"},
		SVAP:   SVAPConfig{Endpoints: map[string]SVAPEndpoint{}},
		Retention: RetentionConfig{
			DefaultTTL: "24h",
			OrgTTLs:    map[string]string{"org": "48h"},
			RoleTTLs:   map[string]string{"reader": "72h", "admin": "96h"},
			UserTTLs:   map[string]string{"user": "120h"},
		},
	})
	if err := service.Apply(context.Background()); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	tests := []struct {
		name    string
		userID  string
		roles   []string
		orgCode string
		want    time.Duration
	}{
		{name: "default", want: 24 * time.Hour},
		{name: "org", orgCode: "org", want: 48 * time.Hour},
		{name: "max role beats org", roles: []string{"reader", "admin"}, orgCode: "org", want: 96 * time.Hour},
		{name: "user beats role", userID: "user", roles: []string{"admin"}, orgCode: "org", want: 120 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.GetRetentionTTL(tt.userID, tt.roles, tt.orgCode)
			if got != tt.want {
				t.Fatalf("GetRetentionTTL() = %s, want %s", got, tt.want)
			}
		})
	}
}
