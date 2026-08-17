package historymetrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type usageFileDocument struct {
	Totals struct {
		ProviderCalls     int64 `json:"provider_calls"`
		TurnsTotal        int64 `json:"turns_total"`
		ValidTurnsTotal   int64 `json:"valid_turns_total"`
		InvalidTurnsTotal int64 `json:"invalid_turns_total"`
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		CacheReadTokens   int64 `json:"cache_read_tokens"`
		CacheWriteTokens  int64 `json:"cache_write_tokens"`
		TotalTokens       int64 `json:"total_tokens"`
	} `json:"totals"`
	RecentEvents []struct {
		EventID                 string `json:"event_id"`
		Kind                    string `json:"kind"`
		ProviderPass            int    `json:"provider_pass"`
		CompileDurationMS       int64  `json:"compile_duration_ms"`
		EstimatedPromptTokens   int64  `json:"estimated_prompt_tokens"`
		ReplayMessageCount      int    `json:"replay_message_count"`
		TTFTMS                  int64  `json:"ttft_ms"`
		DurationMS              int64  `json:"duration_ms"`
		CacheReadTokens         int64  `json:"cache_read_tokens"`
		CacheReadUsageAvailable bool   `json:"cache_read_usage_available"`
	} `json:"recent_events"`
}

func LoadUsageSummary(path string) (Summary, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{}, nil
		}
		return Summary{}, fmt.Errorf("read usage file: %w", err)
	}
	var doc usageFileDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return Summary{}, fmt.Errorf("decode usage file: %w", err)
	}
	totals := Totals{
		InputTokens:        doc.Totals.InputTokens,
		OutputTokens:       doc.Totals.OutputTokens,
		CacheReadTokens:    doc.Totals.CacheReadTokens,
		CacheWriteTokens:   doc.Totals.CacheWriteTokens,
		PromptTokensTotal:  doc.Totals.InputTokens + doc.Totals.CacheReadTokens + doc.Totals.CacheWriteTokens,
		RequestTokensTotal: doc.Totals.TotalTokens,
	}
	metrics := providerPassMetrics(doc.RecentEvents)
	return Summary{
		ProviderCallsTotal:  int(doc.Totals.ProviderCalls),
		TurnsTotal:          int(doc.Totals.TurnsTotal),
		ValidTurnsTotal:     int(doc.Totals.ValidTurnsTotal),
		InvalidTurnsTotal:   int(doc.Totals.InvalidTurnsTotal),
		RequestTokensTotal:  totals.RequestTokensTotal,
		PromptTokensTotal:   totals.PromptTokensTotal,
		CacheReadTokens:     totals.CacheReadTokens,
		CacheWriteTokens:    totals.CacheWriteTokens,
		CacheHitRate:        cacheHitRateFromTotals(totals),
		ProviderPassMetrics: metrics,
	}, nil
}

func providerPassMetrics(events []struct {
	EventID                 string `json:"event_id"`
	Kind                    string `json:"kind"`
	ProviderPass            int    `json:"provider_pass"`
	CompileDurationMS       int64  `json:"compile_duration_ms"`
	EstimatedPromptTokens   int64  `json:"estimated_prompt_tokens"`
	ReplayMessageCount      int    `json:"replay_message_count"`
	TTFTMS                  int64  `json:"ttft_ms"`
	DurationMS              int64  `json:"duration_ms"`
	CacheReadTokens         int64  `json:"cache_read_tokens"`
	CacheReadUsageAvailable bool   `json:"cache_read_usage_available"`
}) []ProviderPassMetric {
	metrics := make([]ProviderPassMetric, 0, len(events))
	for _, event := range events {
		if event.Kind != "provider_call" || !hasProviderPassTiming(event) {
			continue
		}
		metrics = append(metrics, ProviderPassMetric{
			RequestID:               providerMetricRequestID(event.EventID),
			ModelCallID:             providerMetricModelCallID(event.EventID),
			ProviderPass:            event.ProviderPass,
			CompileDurationMS:       event.CompileDurationMS,
			EstimatedPromptTokens:   event.EstimatedPromptTokens,
			ReplayMessageCount:      event.ReplayMessageCount,
			TTFTMS:                  event.TTFTMS,
			DurationMS:              event.DurationMS,
			CacheReadTokens:         event.CacheReadTokens,
			CacheReadUsageAvailable: event.CacheReadUsageAvailable,
		})
	}
	return metrics
}

func hasProviderPassTiming(event struct {
	EventID                 string `json:"event_id"`
	Kind                    string `json:"kind"`
	ProviderPass            int    `json:"provider_pass"`
	CompileDurationMS       int64  `json:"compile_duration_ms"`
	EstimatedPromptTokens   int64  `json:"estimated_prompt_tokens"`
	ReplayMessageCount      int    `json:"replay_message_count"`
	TTFTMS                  int64  `json:"ttft_ms"`
	DurationMS              int64  `json:"duration_ms"`
	CacheReadTokens         int64  `json:"cache_read_tokens"`
	CacheReadUsageAvailable bool   `json:"cache_read_usage_available"`
}) bool {
	return event.CompileDurationMS > 0 || event.DurationMS > 0
}

func providerMetricRequestID(eventID string) string {
	for index := 0; index+1 < len(eventID); index++ {
		if eventID[index:index+2] == "::" {
			return eventID[:index]
		}
	}
	return eventID
}

func providerMetricModelCallID(eventID string) string {
	for index := 0; index+1 < len(eventID); index++ {
		if eventID[index:index+2] == "::" {
			return eventID[index+2:]
		}
	}
	return ""
}
