package svap

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"svap-query-service/backend/internal/config"
	"svap-query-service/backend/internal/model"
)

// Client интерфейс для взаимодействия с внешней системой СВАП
type Client interface {
	// GetAnalyticalAttributes получает перечень аналитических признаков
	GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error)

	// ExecuteQuery выполняет запрос данных (TURN, FSG, ReportGK)
	ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document interface{}) (interface{}, error)
}

type RealClient struct {
	config *config.ConfigService
	client *http.Client
}

func NewRealClient(cfg *config.ConfigService) *RealClient {
	insecureSkipVerify, _ := strconv.ParseBool(os.Getenv("SVAP_INSECURE_SKIP_VERIFY"))

	return &RealClient{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
			},
		},
	}
}

func (c *RealClient) GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	// В реальности тут запрос к НСИ сервису
	return nil, nil
}

type SVAPRequest struct {
	Header   model.SVAPHeader `json:"header"`
	Document interface{}      `json:"document"`
}

func (c *RealClient) ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document interface{}) (interface{}, error) {
	svapCfg := c.config.GetCurrent().SVAP
	endpoint, ok := svapCfg.Endpoints[queryType]
	if !ok {
		// Попробуем найти дефолтный эндпоинт или использовать GKHost как раньше (для совместимости)
		// Но лучше требовать явной настройки.
		return nil, fmt.Errorf("SVAP endpoint not configured for query type %s", queryType)
	}

	host := endpoint.Host
	suffix := endpoint.Suffix

	if host == "" {
		return nil, fmt.Errorf("SVAP host not configured for query type %s", queryType)
	}

	// Формируем полный URL
	fullURL := host
	if suffix != "" {
		if !strings.HasSuffix(fullURL, "/") && !strings.HasPrefix(suffix, "/") {
			fullURL += "/"
		}
		if strings.HasSuffix(fullURL, "/") && strings.HasPrefix(suffix, "/") {
			fullURL = strings.TrimSuffix(fullURL, "/")
		}
		fullURL += suffix
	}

	// Если document это строка (JSON), распарсим её чтобы отправить как объект
	var docObj interface{} = document
	if s, ok := document.(string); ok {
		if err := json.Unmarshal([]byte(s), &docObj); err != nil {
			// Если не JSON, оставляем как есть (возможно это уже подготовленный объект)
			docObj = s
		}
	}

	reqBody := SVAPRequest{
		Header:   header,
		Document: docObj,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SVAP returned error status %s: %s", resp.Status, string(body))
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// MockClient реализация для тестирования
type MockClient struct{}

func (m *MockClient) GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	return []model.AnalyticalAttribute{
		{
			Code:            "BUD_CODE",
			Name:            "Код бюджета",
			Businesses:      []string{"ФБ", "ПУД", "ЕБП"},
			InAccount:       true,
			UseInBalance:    true,
			ValidationType:  "Service",
			ValidationValue: "http://svap-nsi/nsi/budgets",
		},
		{
			Code:            "IKP_CODE",
			Name:            "Код ИКП",
			Businesses:      []string{"ФБ"},
			InAccount:       false,
			UseInBalance:    true,
			ValidationType:  "RegExpression",
			ValidationValue: "([0-9]{4})+(-[0-9]{2})",
		},
	}, nil
}

func (m *MockClient) ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document interface{}) (interface{}, error) {
	// Заглушка для выполнения запросов
	return nil, nil
}
