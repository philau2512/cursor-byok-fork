package forwarder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageFileStoreUpsertIsMemoryOnlyUntilFlush(t *testing.T) {
	directory := t.TempDir()
	store := NewUsageFileStore(directory)
	defer closeUsageStore(t, store)

	if err := store.UpsertEvent(testUsageEvent("request-1::call-1", 10)); err != nil {
		t.Fatalf("UpsertEvent() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, usageFileName)); !os.IsNotExist(err) {
		t.Fatalf("usage file exists before asynchronous flush, stat error = %v", err)
	}
	event, found, err := store.LookupEvent("request-1::call-1")
	if err != nil || !found {
		t.Fatalf("LookupEvent() = (%#v, %t, %v), want in-memory event", event, found, err)
	}
	if event.TotalTokens != 10 {
		t.Fatalf("LookupEvent().TotalTokens = %d, want 10", event.TotalTokens)
	}

	flushUsageStore(t, store)
	if _, err := os.Stat(filepath.Join(directory, usageFileName)); err != nil {
		t.Fatalf("usage file missing after Flush(): %v", err)
	}
}

func TestUsageFileStoreFlushCompactsLegacyEventIndex(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, usageFileName)
	legacy := map[string]any{
		"schema_version": usageFileSchemaVersion,
		"updated_at":     time.Now().UTC(),
		"totals":         usageFileTotals{ProviderCalls: 1, TotalTokens: 7},
		"daily":          []usageFileDaily{{Date: "2026-08-17", ProviderCalls: 1, TotalTokens: 7}},
		"recent_events":  []usageFileEvent{testUsageEvent("request-1::call-1", 7)},
		"event_index":    map[string]usageFileEvent{"request-1::call-1": testUsageEvent("request-1::call-1", 7)},
	}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewUsageFileStore(directory)
	defer closeUsageStore(t, store)
	if err := store.UpsertEvent(testUsageEvent("request-2::call-1", 9)); err != nil {
		t.Fatalf("UpsertEvent() error = %v", err)
	}
	flushUsageStore(t, store)

	body, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("persisted usage is not valid JSON: %v", err)
	}
	if _, found := persisted["event_index"]; found {
		t.Fatal("event_index should not be serialized")
	}
	if len(persisted) == 0 {
		t.Fatal("persisted usage document is empty")
	}

	reloaded := NewUsageFileStore(directory)
	defer closeUsageStore(t, reloaded)
	if event, found, err := reloaded.LookupEvent("request-1::call-1"); err != nil || !found || event.TotalTokens != 7 {
		t.Fatalf("legacy event after reload = (%#v, %t, %v)", event, found, err)
	}
	if event, found, err := reloaded.LookupEvent("request-2::call-1"); err != nil || !found || event.TotalTokens != 9 {
		t.Fatalf("new event after reload = (%#v, %t, %v)", event, found, err)
	}
}

func TestUsageFileStoreUpsertReplacesExistingEventDelta(t *testing.T) {
	store := NewUsageFileStore(t.TempDir())
	defer closeUsageStore(t, store)

	if err := store.UpsertEvent(testUsageEvent("same-event", 10)); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEvent(testUsageEvent("same-event", 25)); err != nil {
		t.Fatal(err)
	}
	store.mu.RLock()
	totals := store.document.Totals
	events := append([]usageFileEvent(nil), store.document.RecentEvents...)
	store.mu.RUnlock()
	if totals.ProviderCalls != 1 || totals.TotalTokens != 25 {
		t.Fatalf("totals = %#v, want one provider call / 25 tokens", totals)
	}
	if len(events) != 1 || events[0].TotalTokens != 25 {
		t.Fatalf("recent events = %#v, want replaced event", events)
	}
}

func TestUsageFileStoreCloseFlushesPendingEvents(t *testing.T) {
	directory := t.TempDir()
	store := NewUsageFileStore(directory)
	if err := store.UpsertEvent(testUsageEvent("pending-close", 11)); err != nil {
		t.Fatal(err)
	}
	closeUsageStore(t, store)

	reloaded := NewUsageFileStore(directory)
	defer closeUsageStore(t, reloaded)
	if event, found, err := reloaded.LookupEvent("pending-close"); err != nil || !found || event.TotalTokens != 11 {
		t.Fatalf("event after Close() = (%#v, %t, %v)", event, found, err)
	}
}

func testUsageEvent(id string, tokens int64) usageFileEvent {
	return usageFileEvent{
		EventID:     id,
		Kind:        usageEventKindProvider,
		At:          time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC),
		InputTokens: tokens,
	}
}

func flushUsageStore(t *testing.T, store *UsageFileStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Flush(ctx); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
}

func closeUsageStore(t *testing.T, store *UsageFileStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
