package model

type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

type Model struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	API            string    `json:"api"`
	Provider       string    `json:"provider"`
	BaseURL        string    `json:"base_url"`
	Reasoning      bool      `json:"reasoning"`
	InputTypes     []string  `json:"input_types"`
	CostPerMillion ModelCost `json:"cost_per_million"`
	ContextWindow  int       `json:"context_window"`
	MaxTokens      int       `json:"max_tokens"`
}

func KimiK25() Model {
	// TODO: add source of the decision for this structure
	return Model{
		ID:            "kimi-k2.5",
		Name:          "Kimi K2.5",
		API:           "openai-completions",
		Provider:      "moonshot",
		BaseURL:       "https://api.moonshot.ai/v1",
		Reasoning:     true,
		InputTypes:    []string{"text", "image"},
		ContextWindow: 262144,
		MaxTokens:     32768,
	}
}
