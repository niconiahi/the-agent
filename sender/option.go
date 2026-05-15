package sender

type ThinkingLevel string

const (
	THINKING_LEVEL_MINIMAL ThinkingLevel = "minimal"
	THINKING_LEVEL_LOW     ThinkingLevel = "low"
	THINKING_LEVEL_MEDIUM  ThinkingLevel = "medium"
	THINKING_LEVEL_HIGH    ThinkingLevel = "high"
)

type StreamOptions struct {
	Temperature   *float64          `json:"temperature,omitempty"`
	MaxTokens     *int              `json:"max_tokens,omitempty"`
	APIKey        string            `json:"api_key"`
	Headers       map[string]string `json:"headers,omitempty"`
	ThinkingLevel ThinkingLevel     `json:"thinking_level,omitempty"`
}
