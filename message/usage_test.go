package message

import (
	"math"
	"testing"
)

func TestCalculateCost_ZeroTokens(t *testing.T) {
	cost := CalculateCost(ModelCost{}, Usage{})

	if cost.Input != 0 || cost.Output != 0 || cost.CacheRead != 0 || cost.CacheWrite != 0 || cost.Total != 0 {
		t.Fatalf("expected all zeros, got %+v", cost)
	}
}

func TestCalculateCost_AllFields(t *testing.T) {
	model_cost := ModelCost{
		Input:      3.0,
		Output:     15.0,
		CacheRead:  0.3,
		CacheWrite: 3.75,
	}
	usage := Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  2000,
		CacheWriteTokens: 100,
	}

	cost := CalculateCost(model_cost, usage)

	assert_float(t, "Input", cost.Input, 0.003)
	assert_float(t, "Output", cost.Output, 0.0075)
	assert_float(t, "CacheRead", cost.CacheRead, 0.0006)
	assert_float(t, "CacheWrite", cost.CacheWrite, 0.000375)
	assert_float(t, "Total", cost.Total, 0.003+0.0075+0.0006+0.000375)
}

func TestCalculateCost_LargeTokenCounts(t *testing.T) {
	cost := CalculateCost(
		ModelCost{Input: 3.0},
		Usage{InputTokens: 1_000_000},
	)

	assert_float(t, "Input", cost.Input, 3.0)
	assert_float(t, "Total", cost.Total, 3.0)
}

func TestCalculateCost_FractionalPricing(t *testing.T) {
	cost := CalculateCost(
		ModelCost{Input: 0.25},
		Usage{InputTokens: 1},
	)

	assert_float(t, "Input", cost.Input, 0.00000025)
	assert_float(t, "Total", cost.Total, 0.00000025)
}

func assert_float(t *testing.T, label string, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("%s: got %v, want %v", label, got, want)
	}
}
