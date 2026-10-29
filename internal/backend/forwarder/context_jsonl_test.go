package forwarder

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestConversationFileStoreJSONLAppendAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	store := NewConversationFileStore(tempDir)
	conversationID := "test-conv-1"

	// 1. Initial append: creates context.jsonl and state.json
	entry1 := HistoryEntry{
		Seq:       1,
		TurnSeq:   1,
		Role:      "user",
		Kind:      "user_message",
		Payload:   json.RawMessage(`{"text":"hello"}`),
		CreatedAt: time.Now().UTC(),
	}
	conv, assigned, err := store.AppendEntries(conversationID, []HistoryEntry{entry1})
	if err != nil {
		t.Fatalf("AppendEntries 1 failed: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("assigned = %d, want 1", len(assigned))
	}
	if conv.ContextVersion != 1 {
		t.Fatalf("ContextVersion = %d, want 1", conv.ContextVersion)
	}

	// Verify context.jsonl was created and context.json does not exist
	jsonlPath := store.contextJSONLPath(conversationID)
	if ok, _ := fileExists(jsonlPath); !ok {
		t.Fatalf("expected context.jsonl to exist at %s", jsonlPath)
	}
	legacyPath := store.contextPath(conversationID)
	if ok, _ := fileExists(legacyPath); ok {
		t.Fatalf("legacy context.json should not exist at %s", legacyPath)
	}

	// 2. Second append: O(1) appends to context.jsonl
	entry2 := HistoryEntry{
		Seq:       2,
		TurnSeq:   1,
		Role:      "assistant",
		Kind:      "assistant_text",
		Payload:   json.RawMessage(`{"text":"hi there"}`),
		CreatedAt: time.Now().UTC(),
	}
	conv, assigned, err = store.AppendEntries(conversationID, []HistoryEntry{entry2})
	if err != nil {
		t.Fatalf("AppendEntries 2 failed: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("assigned = %d, want 1", len(assigned))
	}
	if conv.ContextVersion != 2 {
		t.Fatalf("ContextVersion = %d, want 2", conv.ContextVersion)
	}

	// 3. Load conversation from store
	loaded, err := store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("loaded.Entries = %d, want 2", len(loaded.Entries))
	}
	if loaded.Entries[0].Kind != "user_message" || loaded.Entries[1].Kind != "assistant_text" {
		t.Fatalf("unexpected entry kinds: %v, %v", loaded.Entries[0].Kind, loaded.Entries[1].Kind)
	}
}

func TestConversationFileStoreMigrateFromLegacyJSON(t *testing.T) {
	tempDir := t.TempDir()
	store := NewConversationFileStore(tempDir)
	conversationID := "legacy-conv-1"

	// Create legacy state.json and context.json
	convDir := store.conversationDir(conversationID)
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatalf("create conv dir: %v", err)
	}

	state := ConversationFile{
		SchemaVersion:  conversationSchemaVersion,
		ConversationID: conversationID,
		NextTurnSeq:    2,
		NextEntrySeq:   3,
		ContextVersion: 2,
		Mode:           "agent",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	stateBytes, _ := json.Marshal(state)
	if err := os.WriteFile(store.statePath(conversationID), stateBytes, 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	legacyContext := conversationContextFile{
		SchemaVersion:  conversationSchemaVersion,
		ConversationID: conversationID,
		Version:        2,
		UpdatedAt:      time.Now().UTC(),
		Items: []HistoryEntry{
			{Seq: 1, TurnSeq: 1, Role: "user", Kind: "user_message", Payload: json.RawMessage(`{"text":"old 1"}`)},
			{Seq: 2, TurnSeq: 1, Role: "assistant", Kind: "assistant_text", Payload: json.RawMessage(`{"text":"old 2"}`)},
		},
	}
	contextBytes, _ := json.Marshal(legacyContext)
	if err := os.WriteFile(store.contextPath(conversationID), contextBytes, 0o644); err != nil {
		t.Fatalf("write legacy context.json: %v", err)
	}

	// Load should successfully read legacy context.json
	loaded, err := store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("LoadConversation legacy failed: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("loaded.Entries = %d, want 2", len(loaded.Entries))
	}

	// Appending an entry should migrate to context.jsonl and remove legacy context.json
	entry3 := HistoryEntry{
		Role:    "user",
		Kind:    "user_message",
		Payload: json.RawMessage(`{"text":"new 3"}`),
	}
	updated, assigned, err := store.AppendEntries(conversationID, []HistoryEntry{entry3})
	if err != nil {
		t.Fatalf("AppendEntries on legacy failed: %v", err)
	}
	if len(assigned) != 1 || len(updated.Entries) != 3 {
		t.Fatalf("entries after migration = %d, want 3", len(updated.Entries))
	}

	// context.jsonl should now exist, and legacy context.json should be cleaned up
	if ok, _ := fileExists(store.contextJSONLPath(conversationID)); !ok {
		t.Fatalf("expected context.jsonl to exist after migration")
	}
	if ok, _ := fileExists(store.contextPath(conversationID)); ok {
		t.Fatalf("expected legacy context.json to be deleted after migration")
	}

	// Subsequent load should read migrated context.jsonl
	reloaded, err := store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if len(reloaded.Entries) != 3 {
		t.Fatalf("reloaded entries = %d, want 3", len(reloaded.Entries))
	}
}

func TestConversationFileStoreTornWriteRecovery(t *testing.T) {
	tempDir := t.TempDir()
	store := NewConversationFileStore(tempDir)
	conversationID := "torn-conv-1"

	// Append 2 valid entries
	entry1 := HistoryEntry{Seq: 1, TurnSeq: 1, Role: "user", Kind: "user_message", Payload: json.RawMessage(`{"text":"valid 1"}`)}
	entry2 := HistoryEntry{Seq: 2, TurnSeq: 1, Role: "assistant", Kind: "assistant_text", Payload: json.RawMessage(`{"text":"valid 2"}`)}
	_, _, err := store.AppendEntries(conversationID, []HistoryEntry{entry1, entry2})
	if err != nil {
		t.Fatalf("AppendEntries failed: %v", err)
	}

	// Simulate torn-write by corrupting the last line of context.jsonl (unfinished trailing json)
	jsonlPath := store.contextJSONLPath(conversationID)
	f, err := os.OpenFile(jsonlPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	_, _ = f.WriteString(`{"seq":3, "kind":"tool_call", "incomplete_json":`)
	_ = f.Close()

	// Clear cache so it reads disk
	store.cacheMu.Lock()
	store.entriesCache = make(map[string]cachedConversationEntries)
	store.cacheMu.Unlock()

	// Loading should gracefully ignore the corrupted trailing torn line
	loaded, err := store.LoadConversation(conversationID)
	if err != nil {
		t.Fatalf("LoadConversation after torn-write failed: %v", err)
	}
	if len(loaded.Entries) != 2 {
		t.Fatalf("loaded.Entries = %d, want 2 (discarded torn entry)", len(loaded.Entries))
	}
	if loaded.Entries[1].Kind != "assistant_text" {
		t.Fatalf("last entry kind = %s, want assistant_text", loaded.Entries[1].Kind)
	}
}

func TestConversationFileStoreBenchmarkLargeSession(t *testing.T) {
	tempDir := t.TempDir()
	store := NewConversationFileStore(tempDir)
	conversationID := "benchmark-conv"

	// Create 1000 items
	entries := make([]HistoryEntry, 0, 1000)
	for i := 1; i <= 1000; i++ {
		entries = append(entries, HistoryEntry{
			Seq:       int64(i),
			TurnSeq:   int64((i + 1) / 2),
			Role:      "tool",
			Kind:      "tool_result",
			Payload:   json.RawMessage(fmt.Sprintf(`{"tool_call_id":"tool-%d","output":"some reasonable result output for tool number %d"}`, i, i)),
			CreatedAt: time.Now().UTC(),
		})
	}

	// Initial bulk save
	t0 := time.Now()
	_, _, err := store.AppendEntries(conversationID, entries)
	if err != nil {
		t.Fatalf("initial bulk append failed: %v", err)
	}
	t.Logf("Initial 1000 items append: %v", time.Since(t0))

	// Single append on 1000 items should be under 5ms (previously on 14MB was 200ms)
	newEntry := HistoryEntry{
		Role:    "user",
		Kind:    "user_message",
		Payload: json.RawMessage(`{"text":"how are you?"}`),
	}
	t1 := time.Now()
	conv, assigned, err := store.AppendEntries(conversationID, []HistoryEntry{newEntry})
	dur := time.Since(t1)
	if err != nil {
		t.Fatalf("single append failed: %v", err)
	}
	if len(assigned) != 1 || len(conv.Entries) != 1001 {
		t.Fatalf("entries count = %d, want 1001", len(conv.Entries))
	}
	t.Logf("Append 1 item to 1000-item session took: %v", dur)
	if dur > 50*time.Millisecond {
		t.Errorf("append took %v, expected < 50ms", dur)
	}
}