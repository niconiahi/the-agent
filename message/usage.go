package message

type StopReason string

const (
	STOP_REASON_STOP     StopReason = "stop"
	STOP_REASON_LENGTH   StopReason = "length"
	STOP_REASON_TOOL_USE StopReason = "tool_use"
	STOP_REASON_ERROR    StopReason = "error"
	STOP_REASON_ABORTED  StopReason = "aborted"
)

type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
	Total      float64 `json:"total"`
}

type Usage struct {
	InputTokens      int  `json:"input_tokens"`
	OutputTokens     int  `json:"output_tokens"`
	CacheReadTokens  int  `json:"cache_read_tokens"`
	CacheWriteTokens int  `json:"cache_write_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	Cost             Cost `json:"cost"`
}

type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

func CalculateCost(model_cost ModelCost, usage Usage) Cost {
	input := float64(usage.InputTokens) * model_cost.Input / 1_000_000
	output := float64(usage.OutputTokens) * model_cost.Output / 1_000_000
	cache_read := float64(usage.CacheReadTokens) * model_cost.CacheRead / 1_000_000
	cache_write := float64(usage.CacheWriteTokens) * model_cost.CacheWrite / 1_000_000
	return Cost{
		Input:      input,
		Output:     output,
		CacheRead:  cache_read,
		CacheWrite: cache_write,
		Total:      input + output + cache_read + cache_write,
	}
}
