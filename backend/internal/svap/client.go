package svap

import (
	"context"

	"ledger-aggregator/backend/internal/config"
	"ledger-aggregator/backend/internal/model"
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
}

func NewRealClient(cfg *config.ConfigService) *RealClient {
	return &RealClient{config: cfg}
}

func (c *RealClient) GetAnalyticalAttributes(ctx context.Context) ([]model.AnalyticalAttribute, error) {
	_ = c.config.GetCurrent().SVAP // Используем актуальные хосты
	// Реализация запроса к СВАП НСИ
	return nil, nil
}

func (c *RealClient) ExecuteQuery(ctx context.Context, queryType string, header model.SVAPHeader, document interface{}) (interface{}, error) {
	hosts := c.config.GetCurrent().SVAP
	_ = hosts // Используем hosts.GKHost, hosts.LSHost и т.д.
	// Реализация запроса к конкретному хосту СВАП в зависимости от типа системы
	return nil, nil
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
