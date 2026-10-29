package historymetrics

type Summary struct {
	ProviderCallsTotal  int                  `json:"providerCallsTotal"`
	TurnsTotal          int                  `json:"turnsTotal"`
	ValidTurnsTotal     int                  `json:"validTurnsTotal"`
	InvalidTurnsTotal   int                  `json:"invalidTurnsTotal"`
	RequestTokensTotal  int64                `json:"requestTokensTotal"`
	PromptTokensTotal   int64                `json:"promptTokensTotal"`
	CacheReadTokens     int64                `json:"cacheReadTokens"`
	CacheWriteTokens    int64                `json:"cacheWriteTokens"`
	CacheHitRate        *float64             `json:"cacheHitRate"`
	ProviderPassMetrics []ProviderPassMetric `json:"providerPassMetrics,omitempty"`
}

type ProviderPassMetric struct {
	RequestID               string `json:"requestID"`
	ModelCallID             string `json:"modelCallID"`
	ProviderPass            int    `json:"providerPass"`
	CompileDurationMS       int64  `json:"compileDurationMS"`
	EstimatedPromptTokens   int64  `json:"estimatedPromptTokens"`
	ReplayMessageCount      int    `json:"replayMessageCount"`
	TTFTMS                  int64  `json:"ttftMS"`
	DurationMS              int64  `json:"durationMS"`
	CacheReadTokens         int64  `json:"cacheReadTokens"`
	CacheReadUsageAvailable bool   `json:"cacheReadUsageAvailable"`
}

type Totals struct {
	InputTokens        int64
	OutputTokens       int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	PromptTokensTotal  int64
	RequestTokensTotal int64
}

func cacheHitRateFromTotals(totals Totals) *float64 {
	inputCacheTokensTotal := totals.CacheReadTokens + totals.InputTokens
	if inputCacheTokensTotal <= 0 {
		return nil
	}
	value := float64(totals.CacheReadTokens) / float64(inputCacheTokensTotal)
	return &value
}
