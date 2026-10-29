package forwarder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	usageFileName          = "usage.json"
	usageFileSchemaVersion = 2
	usageRecentEventLimit  = 500
	usageWriteDebounce     = 500 * time.Millisecond
	usageWriteRetryDelay   = time.Second

	usageEventKindProvider = "provider_call"
	usageEventKindTurn     = "turn_finalized"
	usageTurnStatusDone    = "completed"
)

type UsageFileStore struct {
	path string

	mu         sync.RWMutex
	document   usageFileDocument
	eventIndex map[string]usageFileEvent
	generation uint64
	persisted  uint64
	lastError  error

	writeMu sync.Mutex
	dirty   chan struct{}
	stop    chan struct{}
	done    chan struct{}
	close   sync.Once
}

type usageFileDocument struct {
	SchemaVersion int              `json:"schema_version"`
	UpdatedAt     time.Time        `json:"updated_at"`
	Totals        usageFileTotals  `json:"totals"`
	Daily         []usageFileDaily `json:"daily"`
	RecentEvents  []usageFileEvent `json:"recent_events"`
}

type usageFileTotals struct {
	ProviderCalls     int64 `json:"provider_calls"`
	TurnsTotal        int64 `json:"turns_total"`
	ValidTurnsTotal   int64 `json:"valid_turns_total"`
	InvalidTurnsTotal int64 `json:"invalid_turns_total"`
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CacheReadTokens   int64 `json:"cache_read_tokens"`
	CacheWriteTokens  int64 `json:"cache_write_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

type usageFileDaily struct {
	Date              string `json:"date"`
	ProviderCalls     int64  `json:"provider_calls"`
	TurnsTotal        int64  `json:"turns_total"`
	ValidTurnsTotal   int64  `json:"valid_turns_total"`
	InvalidTurnsTotal int64  `json:"invalid_turns_total"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	CacheWriteTokens  int64  `json:"cache_write_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
}

type usageFileEvent struct {
	EventID                 string    `json:"event_id"`
	Kind                    string    `json:"kind,omitempty"`
	Status                  string    `json:"status,omitempty"`
	At                      time.Time `json:"at"`
	InputTokens             int64     `json:"input_tokens"`
	OutputTokens            int64     `json:"output_tokens"`
	CacheReadTokens         int64     `json:"cache_read_tokens"`
	CacheWriteTokens        int64     `json:"cache_write_tokens"`
	TotalTokens             int64     `json:"total_tokens"`
	UsagePresent            bool      `json:"usage_present"`
	ProviderPass            int       `json:"provider_pass,omitempty"`
	CompileDurationMS       int64     `json:"compile_duration_ms,omitempty"`
	EstimatedPromptTokens   int64     `json:"estimated_prompt_tokens,omitempty"`
	ReplayMessageCount      int       `json:"replay_message_count,omitempty"`
	TTFTMS                  int64     `json:"ttft_ms,omitempty"`
	DurationMS              int64     `json:"duration_ms,omitempty"`
	CacheReadUsageAvailable bool      `json:"cache_read_usage_available,omitempty"`
}

type usageFileDelta struct {
	providerCalls     int64
	turnsTotal        int64
	validTurnsTotal   int64
	invalidTurnsTotal int64
	inputTokens       int64
	outputTokens      int64
	cacheReadTokens   int64
	cacheWriteTokens  int64
	totalTokens       int64
}

func NewUsageFileStore(historyRoot string) *UsageFileStore {
	store := &UsageFileStore{
		path:       filepath.Join(strings.TrimSpace(historyRoot), usageFileName),
		dirty:      make(chan struct{}, 1),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		eventIndex: make(map[string]usageFileEvent),
	}
	store.document = newUsageFileDocument()
	if strings.TrimSpace(store.path) != "" {
		document, err := readUsageFileDocument(store.path)
		if err != nil {
			store.lastError = err
			log.Printf("forwarder usage store load failed path=%s err=%v", store.path, err)
		} else {
			store.document = document
			store.eventIndex = buildUsageEventIndex(document.RecentEvents)
		}
	}
	go store.runWriter()
	return store
}

func (store *UsageFileStore) UpsertEvent(event usageFileEvent) error {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return nil
	}
	event = normalizeUsageFileEvent(event)
	if event.EventID == "" {
		return nil
	}

	store.mu.Lock()
	oldEvent, found := store.eventIndex[event.EventID]
	if found {
		applyUsageFileDelta(&store.document, oldEvent.At, negateUsageFileDelta(usageFileEventDelta(oldEvent)))
	}
	applyUsageFileDelta(&store.document, event.At, usageFileEventDelta(event))
	store.document.RecentEvents = upsertRecentUsageEvent(store.document.RecentEvents, event)
	store.document.RecentEvents = trimRecentUsageEvents(store.document.RecentEvents, usageRecentEventLimit)
	store.eventIndex = buildUsageEventIndex(store.document.RecentEvents)
	store.document.SchemaVersion = usageFileSchemaVersion
	store.document.UpdatedAt = time.Now().UTC()
	store.generation++
	store.mu.Unlock()
	store.signalDirty()
	return nil
}

func (store *UsageFileStore) LookupEvent(needle string) (usageFileEvent, bool, error) {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return usageFileEvent{}, false, nil
	}
	trimmed := strings.TrimSpace(needle)
	if trimmed == "" {
		return usageFileEvent{}, false, nil
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	var aggregate usageFileEvent
	found := false
	for eventID, event := range store.eventIndex {
		if eventID != trimmed && !strings.HasPrefix(eventID, trimmed+"::") {
			continue
		}
		if !found {
			aggregate = usageFileEvent{EventID: trimmed, At: event.At}
			found = true
		}
		if event.At.After(aggregate.At) || event.At.Equal(aggregate.At) {
			aggregate.At = event.At
			aggregate.ProviderPass = event.ProviderPass
			aggregate.CompileDurationMS = event.CompileDurationMS
			aggregate.EstimatedPromptTokens = event.EstimatedPromptTokens
			aggregate.ReplayMessageCount = event.ReplayMessageCount
			aggregate.TTFTMS = event.TTFTMS
			aggregate.DurationMS = event.DurationMS
			aggregate.CacheReadUsageAvailable = event.CacheReadUsageAvailable
		}
		aggregate.InputTokens += nonNegativeInt64(event.InputTokens)
		aggregate.OutputTokens += nonNegativeInt64(event.OutputTokens)
		aggregate.CacheReadTokens += nonNegativeInt64(event.CacheReadTokens)
		aggregate.CacheWriteTokens += nonNegativeInt64(event.CacheWriteTokens)
		aggregate.TotalTokens += nonNegativeInt64(event.TotalTokens)
		aggregate.UsagePresent = aggregate.UsagePresent || event.UsagePresent
	}
	return aggregate, found, nil
}

func (store *UsageFileStore) Flush(ctx context.Context) error {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return store.flushDirty()
}

func (store *UsageFileStore) Close(ctx context.Context) error {
	if store == nil {
		return nil
	}
	store.close.Do(func() { close(store.stop) })
	select {
	case <-store.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return store.Flush(ctx)
}

func (store *UsageFileStore) signalDirty() {
	select {
	case store.dirty <- struct{}{}:
	default:
	}
}

func (store *UsageFileStore) runWriter() {
	defer close(store.done)
	for {
		select {
		case <-store.stop:
			if err := store.flushDirty(); err != nil {
				log.Printf("forwarder usage store shutdown flush failed path=%s err=%v", store.path, err)
			}
			return
		case <-store.dirty:
		}

		timer := time.NewTimer(usageWriteDebounce)
		select {
		case <-store.stop:
			timer.Stop()
			if err := store.flushDirty(); err != nil {
				log.Printf("forwarder usage store shutdown flush failed path=%s err=%v", store.path, err)
			}
			return
		case <-timer.C:
		}
		if err := store.flushDirty(); err != nil {
			log.Printf("forwarder usage store flush failed path=%s err=%v", store.path, err)
			timer := time.NewTimer(usageWriteRetryDelay)
			select {
			case <-store.stop:
				timer.Stop()
				if err := store.flushDirty(); err != nil {
					log.Printf("forwarder usage store shutdown flush failed path=%s err=%v", store.path, err)
				}
				return
			case <-timer.C:
				store.signalDirty()
			}
		}
	}
}

func (store *UsageFileStore) flushDirty() error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	for {
		store.mu.RLock()
		if store.persisted == store.generation {
			store.mu.RUnlock()
			return nil
		}
		generation := store.generation
		snapshot := cloneUsageFileDocument(store.document)
		store.mu.RUnlock()

		if err := writeUsageFileDocument(store.path, snapshot); err != nil {
			store.mu.Lock()
			store.lastError = err
			store.mu.Unlock()
			return err
		}

		store.mu.Lock()
		store.lastError = nil
		if generation > store.persisted {
			store.persisted = generation
		}
		upToDate := store.persisted == store.generation
		store.mu.Unlock()
		if upToDate {
			return nil
		}
	}
}

func newUsageFileDocument() usageFileDocument {
	return usageFileDocument{
		SchemaVersion: usageFileSchemaVersion,
		Daily:         make([]usageFileDaily, 0),
		RecentEvents:  make([]usageFileEvent, 0),
	}
}

func normalizeUsageFileEvent(event usageFileEvent) usageFileEvent {
	event.EventID = strings.TrimSpace(event.EventID)
	event.Kind = normalizeUsageEventKind(event.Kind)
	event.Status = strings.TrimSpace(event.Status)
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	} else {
		event.At = event.At.UTC()
	}
	event.InputTokens = nonNegativeInt64(event.InputTokens)
	event.OutputTokens = nonNegativeInt64(event.OutputTokens)
	event.CacheReadTokens = nonNegativeInt64(event.CacheReadTokens)
	event.CacheWriteTokens = nonNegativeInt64(event.CacheWriteTokens)
	event.TotalTokens = event.InputTokens + event.OutputTokens + event.CacheReadTokens + event.CacheWriteTokens
	return event
}

func readUsageFileDocument(path string) (usageFileDocument, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newUsageFileDocument(), nil
		}
		return usageFileDocument{}, fmt.Errorf("read usage file: %w", err)
	}
	var doc usageFileDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return usageFileDocument{}, fmt.Errorf("decode usage file: %w", err)
	}
	if doc.SchemaVersion == 0 {
		doc.SchemaVersion = 1
	}
	doc.RecentEvents = trimRecentUsageEvents(doc.RecentEvents, usageRecentEventLimit)
	for index := range doc.RecentEvents {
		doc.RecentEvents[index] = normalizeUsageFileEvent(doc.RecentEvents[index])
	}
	if doc.Daily == nil {
		doc.Daily = make([]usageFileDaily, 0)
	}
	if doc.RecentEvents == nil {
		doc.RecentEvents = make([]usageFileEvent, 0)
	}
	return doc, nil
}

func writeUsageFileDocument(path string, document usageFileDocument) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create usage directory: %w", err)
	}
	release, err := acquireConversationLock(path + ".lock")
	if err != nil {
		return err
	}
	defer release()
	return writeJSONFileAtomic(path, document)
}

func cloneUsageFileDocument(document usageFileDocument) usageFileDocument {
	clone := document
	clone.Daily = append([]usageFileDaily(nil), document.Daily...)
	clone.RecentEvents = append([]usageFileEvent(nil), document.RecentEvents...)
	return clone
}

func upsertRecentUsageEvent(items []usageFileEvent, event usageFileEvent) []usageFileEvent {
	event.EventID = strings.TrimSpace(event.EventID)
	if event.EventID == "" {
		return items
	}
	next := make([]usageFileEvent, 0, len(items)+1)
	next = append(next, event)
	for _, item := range items {
		if strings.TrimSpace(item.EventID) == event.EventID {
			continue
		}
		next = append(next, item)
	}
	return next
}

func trimRecentUsageEvents(items []usageFileEvent, limit int) []usageFileEvent {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func buildUsageEventIndex(items []usageFileEvent) map[string]usageFileEvent {
	index := make(map[string]usageFileEvent, len(items))
	for _, event := range items {
		event.EventID = strings.TrimSpace(event.EventID)
		if event.EventID == "" {
			continue
		}
		event.Kind = normalizeUsageEventKind(event.Kind)
		index[event.EventID] = event
	}
	return index
}

func normalizeUsageEventKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case usageEventKindTurn:
		return usageEventKindTurn
	default:
		return usageEventKindProvider
	}
}

func usageFileEventDelta(event usageFileEvent) usageFileDelta {
	switch normalizeUsageEventKind(event.Kind) {
	case usageEventKindTurn:
		delta := usageFileDelta{turnsTotal: 1}
		if strings.TrimSpace(event.Status) == usageTurnStatusDone {
			delta.validTurnsTotal = 1
		} else {
			delta.invalidTurnsTotal = 1
		}
		return delta
	default:
		return usageFileDelta{
			providerCalls:    1,
			inputTokens:      nonNegativeInt64(event.InputTokens),
			outputTokens:     nonNegativeInt64(event.OutputTokens),
			cacheReadTokens:  nonNegativeInt64(event.CacheReadTokens),
			cacheWriteTokens: nonNegativeInt64(event.CacheWriteTokens),
			totalTokens:      nonNegativeInt64(event.TotalTokens),
		}
	}
}

func negateUsageFileDelta(value usageFileDelta) usageFileDelta {
	return usageFileDelta{
		providerCalls:     -value.providerCalls,
		turnsTotal:        -value.turnsTotal,
		validTurnsTotal:   -value.validTurnsTotal,
		invalidTurnsTotal: -value.invalidTurnsTotal,
		inputTokens:       -value.inputTokens,
		outputTokens:      -value.outputTokens,
		cacheReadTokens:   -value.cacheReadTokens,
		cacheWriteTokens:  -value.cacheWriteTokens,
		totalTokens:       -value.totalTokens,
	}
}

func applyUsageFileDelta(doc *usageFileDocument, at time.Time, delta usageFileDelta) {
	if doc == nil {
		return
	}
	doc.Totals.ProviderCalls = clampNonNegativeInt64(doc.Totals.ProviderCalls + delta.providerCalls)
	doc.Totals.TurnsTotal = clampNonNegativeInt64(doc.Totals.TurnsTotal + delta.turnsTotal)
	doc.Totals.ValidTurnsTotal = clampNonNegativeInt64(doc.Totals.ValidTurnsTotal + delta.validTurnsTotal)
	doc.Totals.InvalidTurnsTotal = clampNonNegativeInt64(doc.Totals.InvalidTurnsTotal + delta.invalidTurnsTotal)
	doc.Totals.InputTokens = clampNonNegativeInt64(doc.Totals.InputTokens + delta.inputTokens)
	doc.Totals.OutputTokens = clampNonNegativeInt64(doc.Totals.OutputTokens + delta.outputTokens)
	doc.Totals.CacheReadTokens = clampNonNegativeInt64(doc.Totals.CacheReadTokens + delta.cacheReadTokens)
	doc.Totals.CacheWriteTokens = clampNonNegativeInt64(doc.Totals.CacheWriteTokens + delta.cacheWriteTokens)
	doc.Totals.TotalTokens = clampNonNegativeInt64(doc.Totals.TotalTokens + delta.totalTokens)

	date := at.UTC().Format("2006-01-02")
	for index := range doc.Daily {
		if doc.Daily[index].Date != date {
			continue
		}
		applyUsageDailyDelta(&doc.Daily[index], delta)
		return
	}
	item := usageFileDaily{Date: date}
	applyUsageDailyDelta(&item, delta)
	doc.Daily = append(doc.Daily, item)
}

func applyUsageDailyDelta(item *usageFileDaily, delta usageFileDelta) {
	if item == nil {
		return
	}
	item.ProviderCalls = clampNonNegativeInt64(item.ProviderCalls + delta.providerCalls)
	item.TurnsTotal = clampNonNegativeInt64(item.TurnsTotal + delta.turnsTotal)
	item.ValidTurnsTotal = clampNonNegativeInt64(item.ValidTurnsTotal + delta.validTurnsTotal)
	item.InvalidTurnsTotal = clampNonNegativeInt64(item.InvalidTurnsTotal + delta.invalidTurnsTotal)
	item.InputTokens = clampNonNegativeInt64(item.InputTokens + delta.inputTokens)
	item.OutputTokens = clampNonNegativeInt64(item.OutputTokens + delta.outputTokens)
	item.CacheReadTokens = clampNonNegativeInt64(item.CacheReadTokens + delta.cacheReadTokens)
	item.CacheWriteTokens = clampNonNegativeInt64(item.CacheWriteTokens + delta.cacheWriteTokens)
	item.TotalTokens = clampNonNegativeInt64(item.TotalTokens + delta.totalTokens)
}

func clampNonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
