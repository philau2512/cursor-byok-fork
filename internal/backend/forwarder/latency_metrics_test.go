package forwarder

import (
	"testing"
	"time"
)

func TestRecordTurnUsageSnapshotPersistsPerPassLatency(t *testing.T) {
	store := NewConversationFileStore(t.TempDir())
	conversation := testConversation(nil)
	if _, _, err := store.AppendEntries(conversation.ConversationID, nil); err != nil {
		t.Fatalf("AppendEntries() error = %v", err)
	}
	service := &Service{store: store, usageStore: NewUsageFileStore(store.HistoryDir())}
	preparedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	firstEventAt := preparedAt.Add(125 * time.Millisecond)
	finishedAt := preparedAt.Add(900 * time.Millisecond)
	stream := &ActiveStream{
		ConversationID:         conversation.ConversationID,
		ModelID:                "configured-model",
		ModelName:              "display-model",
		UpdatedAt:              finishedAt,
		CheckpointConversation: conversation,
	}
	usage := turnUsageSnapshot{
		Provider:              "openai",
		Model:                 "provider-model",
		InputTokens:           100,
		OutputTokens:          25,
		CacheReadTokens:       75,
		CacheReadPresent:      true,
		UsagePresent:          true,
		ProviderPass:          3,
		CompileDurationMS:     18,
		EstimatedPromptTokens: 220,
		ReplayMessageCount:    12,
		RequestPreparedAt:     preparedAt,
		FirstEventAt:          firstEventAt,
	}
	if err := service.recordTurnUsageSnapshot(stream, conversation.ConversationID, 1, "request-1", "call-3", "completed", usage, "", false); err != nil {
		t.Fatalf("recordTurnUsageSnapshot() error = %v", err)
	}

	loaded, err := store.LoadConversation(conversation.ConversationID)
	if err != nil {
		t.Fatalf("LoadConversation() error = %v", err)
	}
	got := loaded.LastProviderCall
	if got == nil {
		t.Fatal("LastProviderCall was not persisted")
	}
	if got.ProviderPass != 3 || got.CompileDurationMS != 18 || got.EstimatedPromptTokens != 220 || got.ReplayMessageCount != 12 {
		t.Fatalf("persisted pass metadata = %#v", got)
	}
	if got.TTFTMS != 125 || got.DurationMS != 900 || !got.CacheReadUsageAvailable || got.CacheReadTokens != 75 {
		t.Fatalf("persisted latency/cache metadata = %#v", got)
	}

	event, found, err := service.usageStore.LookupEvent("request-1::call-3")
	if err != nil {
		t.Fatalf("LookupEvent() error = %v", err)
	}
	if !found {
		t.Fatal("provider usage event was not persisted")
	}
	if event.ProviderPass != 3 || event.TTFTMS != 125 || event.DurationMS != 900 || !event.CacheReadUsageAvailable {
		t.Fatalf("provider usage event = %#v", event)
	}
}

func TestDurationMillisecondsTreatsMissingOrNegativeTimesAsUnavailable(t *testing.T) {
	startedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	if got := durationMilliseconds(time.Time{}, startedAt); got != 0 {
		t.Fatalf("duration with missing start = %d, want 0", got)
	}
	if got := durationMilliseconds(startedAt, startedAt.Add(-time.Millisecond)); got != 0 {
		t.Fatalf("duration with negative interval = %d, want 0", got)
	}
}
