package svap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"svap-query-service/backend/internal/config"
	"svap-query-service/backend/internal/model"
)

func TestRealClientUsesUnifiedFahMainRequestShape(t *testing.T) {
	tests := []struct {
		name      string
		queryType string
	}{
		{name: "FSG uses header/document envelope", queryType: "FSG"},
		{name: "TURN uses header/document envelope", queryType: "TURN"},
		{name: "COR uses header/document envelope", queryType: "COR"},
		{name: "PA uses header/document envelope", queryType: "PA"},
		{name: "CONS uses header/document envelope", queryType: "CONS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"document":{"RespData":[]}}`))
			}))
			defer server.Close()

			cfg := config.NewConfigService(nil)
			cfg.UpdatePending(config.AppConfig{
				SVAP: config.SVAPConfig{Endpoints: map[string]config.SVAPEndpoint{
					tt.queryType: {Host: server.URL},
				}},
			})
			if err := cfg.Apply(context.Background()); err != nil {
				t.Fatalf("apply config: %v", err)
			}

			client := NewRealClient(cfg)
			_, err := client.ExecuteQuery(context.Background(), tt.queryType, model.SVAPHeader{RequestType: tt.queryType}, map[string]any{"Book": "FTOperFed"})
			if err != nil {
				t.Fatalf("ExecuteQuery() error = %v", err)
			}

			if _, hasHeader := got["header"]; !hasHeader {
				t.Fatalf("header missing; body %#v", got)
			}
			document, hasDocument := got["document"].(map[string]any)
			if !hasDocument {
				t.Fatalf("document missing; body %#v", got)
			}
			if document["Book"] != "FTOperFed" {
				t.Fatalf("document body = %#v, want Book inside document", document)
			}
		})
	}
}
