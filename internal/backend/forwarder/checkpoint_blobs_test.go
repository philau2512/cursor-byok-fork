package forwarder

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"cursor/gen/agentv1"
)

func TestCheckpointBlobSyncWaitsForAcknowledgementsBeforePublishingNonTerminalCheckpoint(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	if err := service.queueCheckpointProjection(stream, projection, nil); err != nil {
		t.Fatalf("queueCheckpointProjection() error = %v", err)
	}
	events := readCheckpointTestEvents(t, service, stream)
	if len(events) != len(projection.Blobs) {
		t.Fatalf("events before ACK = %d, want %d Blob writes", len(events), len(projection.Blobs))
	}
	for _, event := range events {
		if event.Message.GetKvServerMessage().GetSetBlobArgs() == nil {
			t.Fatalf("event before ACK = %#v, want set_blob_args", event.Message)
		}
	}

	stream.mu.Lock()
	var firstRequestID uint32
	for requestID := range stream.PendingCheckpointBlobWrites {
		firstRequestID = requestID
		break
	}
	stream.mu.Unlock()
	if firstRequestID == 0 {
		t.Fatal("checkpoint projection has no pending Blob writes")
	}
	if err := service.handleCheckpointBlobResult(stream, &agentv1.KvClientMessage{
		Id: firstRequestID,
		Message: &agentv1.KvClientMessage_SetBlobResult{
			SetBlobResult: &agentv1.SetBlobResult{},
		},
	}); err != nil {
		t.Fatalf("first Blob ACK error = %v", err)
	}
	for _, event := range readCheckpointTestEvents(t, service, stream) {
		if event.Message.GetConversationCheckpointUpdate() != nil {
			t.Fatal("checkpoint published after only a partial Blob acknowledgement")
		}
	}

	acknowledgeCheckpointBlobs(t, service, stream)
	events = readCheckpointTestEvents(t, service, stream)
	checkpointCount := 0
	for _, event := range events {
		if event.Message.GetConversationCheckpointUpdate() != nil {
			checkpointCount++
		}
	}
	if checkpointCount != 1 {
		t.Fatalf("checkpoints after ACK = %d, want 1", checkpointCount)
	}
}

func TestCheckpointBlobSyncPublishesCheckpointBeforeSuccessfulTerminal(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	completion := &pendingTurnCompletion{
		RequestID: stream.RequestID,
		Usage:     turnUsageSnapshot{InputTokens: 11, OutputTokens: 7},
	}
	if err := service.queueCheckpointProjection(stream, projection, completion); err != nil {
		t.Fatalf("queueCheckpointProjection() error = %v", err)
	}
	eventsBeforeACK := readCheckpointTestEvents(t, service, stream)
	for _, event := range eventsBeforeACK {
		if event.Message.GetConversationCheckpointUpdate() != nil || event.Message.GetInteractionUpdate().GetTurnEnded() != nil || event.End {
			t.Fatalf("event before ACK = %#v, want only Blob writes", event)
		}
	}
	acknowledgeCheckpointBlobs(t, service, stream)

	events := readCheckpointTestEvents(t, service, stream)
	checkpointIndex, turnEndedIndex, endIndex := -1, -1, -1
	for index, event := range events {
		switch {
		case event.Message.GetConversationCheckpointUpdate() != nil:
			checkpointIndex = index
		case event.Message.GetInteractionUpdate().GetTurnEnded() != nil:
			turnEndedIndex = index
		case event.End:
			endIndex = index
		}
	}
	if checkpointIndex < 0 || turnEndedIndex <= checkpointIndex || endIndex <= turnEndedIndex {
		t.Fatalf("terminal order checkpoint=%d turn_ended=%d end=%d", checkpointIndex, turnEndedIndex, endIndex)
	}
}

func TestCheckpointBlobTimeoutDoesNotFailSuccessfulTurn(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	completion := &pendingTurnCompletion{
		RequestID: stream.RequestID,
		Usage:     turnUsageSnapshot{InputTokens: 11, OutputTokens: 7},
	}
	if err := service.queueCheckpointProjection(stream, projection, completion); err != nil {
		t.Fatalf("queueCheckpointProjection() error = %v", err)
	}
	if err := service.handleCheckpointBlobTimeout(stream); err != nil {
		t.Fatalf("handleCheckpointBlobTimeout() error = %v", err)
	}

	events := readCheckpointTestEvents(t, service, stream)
	var checkpoint, turnEnded, successfulEnd bool
	for _, event := range events {
		checkpoint = checkpoint || event.Message.GetConversationCheckpointUpdate() != nil
		turnEnded = turnEnded || event.Message.GetInteractionUpdate().GetTurnEnded() != nil
		successfulEnd = successfulEnd || event.End && event.TerminalErrorCode == ""
	}
	if checkpoint || !turnEnded || !successfulEnd {
		t.Fatalf("timeout events checkpoint=%v turn_ended=%v successful_end=%v", checkpoint, turnEnded, successfulEnd)
	}
}

func TestCheckpointBlobSyncPublishesCheckpointBeforeFailedTerminal(t *testing.T) {
	service, stream, _ := testCheckpointBlobProjection(t)
	if err := service.failActiveStream(
		stream,
		stream.ConversationID,
		stream.RequestID,
		"model-call-1",
		"provider_error",
		"provider failed",
	); err != nil {
		t.Fatalf("failActiveStream() error = %v", err)
	}

	for _, event := range readCheckpointTestEvents(t, service, stream) {
		if event.Message.GetConversationCheckpointUpdate() != nil || event.End {
			t.Fatalf("event before ACK = %#v, want only Blob writes", event)
		}
	}
	stream.mu.Lock()
	phaseBeforeACK := stream.Phase
	statusBeforeACK := stream.Status
	stream.mu.Unlock()
	if phaseBeforeACK != TurnPhaseCheckpointing || isTerminalStreamStatus(statusBeforeACK) {
		t.Fatalf("before ACK phase=%s status=%s, want checkpointing and non-terminal", phaseBeforeACK, statusBeforeACK)
	}

	acknowledgeCheckpointBlobs(t, service, stream)
	events := readCheckpointTestEvents(t, service, stream)
	checkpointIndex, endIndex := -1, -1
	for index, event := range events {
		switch {
		case event.Message.GetConversationCheckpointUpdate() != nil:
			checkpointIndex = index
		case event.End:
			endIndex = index
			if event.TerminalErrorCode != "provider_error" || event.TerminalErrorMessage != "provider failed" {
				t.Fatalf("terminal event = %#v, want provider error", event)
			}
		}
	}
	if checkpointIndex < 0 || endIndex <= checkpointIndex {
		t.Fatalf("terminal order checkpoint=%d end=%d", checkpointIndex, endIndex)
	}
	stream.mu.Lock()
	phaseAfterACK := stream.Phase
	statusAfterACK := stream.Status
	stream.mu.Unlock()
	if phaseAfterACK != TurnPhaseFailed || statusAfterACK != StreamStatusFailed {
		t.Fatalf("after ACK phase=%s status=%s, want failed", phaseAfterACK, statusAfterACK)
	}
}

func TestCheckpointBlobTimeoutStillPublishesFailedTerminal(t *testing.T) {
	service, stream, _ := testCheckpointBlobProjection(t)
	if err := service.failActiveStream(
		stream,
		stream.ConversationID,
		stream.RequestID,
		"model-call-1",
		"provider_error",
		"provider failed",
	); err != nil {
		t.Fatalf("failActiveStream() error = %v", err)
	}
	if err := service.handleCheckpointBlobTimeout(stream); err != nil {
		t.Fatalf("handleCheckpointBlobTimeout() error = %v", err)
	}

	events := readCheckpointTestEvents(t, service, stream)
	var checkpoint, failedEnd bool
	for _, event := range events {
		checkpoint = checkpoint || event.Message.GetConversationCheckpointUpdate() != nil
		failedEnd = failedEnd || event.End && event.TerminalErrorCode == "provider_error" && event.TerminalErrorMessage == "provider failed"
	}
	if checkpoint || !failedEnd {
		t.Fatalf("timeout events checkpoint=%v failed_end=%v", checkpoint, failedEnd)
	}
}

func TestManualCompactionNoopWaitsForCheckpointBeforeTerminal(t *testing.T) {
	service, stream, _ := testCheckpointBlobProjection(t)
	conversation, _, _, err := service.snapshotCheckpointConversation(stream)
	if err != nil {
		t.Fatalf("snapshotCheckpointConversation() error = %v", err)
	}
	if _, err := service.store.SaveConversationWithEntries(stream.ConversationID, conversation, conversation.Entries); err != nil {
		t.Fatalf("SaveConversationWithEntries() error = %v", err)
	}
	if err := service.finishManualCompactionNoop(stream); err != nil {
		t.Fatalf("finishManualCompactionNoop() error = %v", err)
	}

	for _, event := range readCheckpointTestEvents(t, service, stream) {
		if event.Message.GetInteractionUpdate().GetTurnEnded() != nil || event.End {
			t.Fatalf("terminal event before checkpoint Blob ACK = %#v", event)
		}
	}
	acknowledgeCheckpointBlobs(t, service, stream)

	events := readCheckpointTestEvents(t, service, stream)
	checkpointIndex, turnEndedIndex, endIndex := -1, -1, -1
	for index, event := range events {
		switch {
		case event.Message.GetConversationCheckpointUpdate() != nil:
			checkpointIndex = index
		case event.Message.GetInteractionUpdate().GetTurnEnded() != nil:
			turnEndedIndex = index
		case event.End:
			endIndex = index
		}
	}
	if checkpointIndex < 0 || turnEndedIndex <= checkpointIndex || endIndex <= turnEndedIndex {
		t.Fatalf("terminal order checkpoint=%d turn_ended=%d end=%d", checkpointIndex, turnEndedIndex, endIndex)
	}
}

func TestCancellationDiscardsUnpublishedCheckpointAndIgnoresLateAcknowledgements(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	if err := service.queueCheckpointProjection(stream, projection, nil); err != nil {
		t.Fatalf("queueCheckpointProjection() error = %v", err)
	}
	eventsBeforeCancel := readCheckpointTestEvents(t, service, stream)
	checkpointBeforeCancel := 0
	for _, event := range eventsBeforeCancel {
		if event.Message.GetConversationCheckpointUpdate() != nil {
			checkpointBeforeCancel++
		}
	}
	if checkpointBeforeCancel != 0 {
		t.Fatalf("checkpoints before cancel = %d, want 0", checkpointBeforeCancel)
	}
	stream.mu.Lock()
	requestIDs := make([]uint32, 0, len(stream.PendingCheckpointBlobWrites))
	for requestID := range stream.PendingCheckpointBlobWrites {
		requestIDs = append(requestIDs, requestID)
	}
	stream.mu.Unlock()
	if err := service.handleCancelIntent(InboundIntent{
		Kind:         "cancel",
		RequestID:    stream.RequestID,
		CancelReason: "user stopped",
	}); err != nil {
		t.Fatalf("handleCancelIntent() error = %v", err)
	}
	for _, requestID := range requestIDs {
		if err := service.handleCheckpointBlobResult(stream, &agentv1.KvClientMessage{
			Id: requestID,
			Message: &agentv1.KvClientMessage_SetBlobResult{
				SetBlobResult: &agentv1.SetBlobResult{},
			},
		}); err != nil {
			t.Fatalf("late ACK %d error = %v", requestID, err)
		}
	}

	events := readCheckpointTestEvents(t, service, stream)
	checkpointCount := 0
	var canceledEnd bool
	for _, event := range events {
		if event.Message.GetConversationCheckpointUpdate() != nil {
			checkpointCount++
		}
		canceledEnd = canceledEnd || event.End && event.TerminalErrorCode == "canceled"
	}
	stream.mu.Lock()
	pending := stream.PendingCheckpoint
	stream.mu.Unlock()
	if checkpointCount != 0 || !canceledEnd || pending != nil {
		t.Fatalf("cancel events checkpoints=%d canceled_end=%v pending=%v", checkpointCount, canceledEnd, pending != nil)
	}
}

func TestCheckpointBlobSyncCoalescesLatestProjectionAndLateAcknowledgement(t *testing.T) {
	service, stream, firstProjection := testCheckpointBlobProjection(t)
	if err := service.queueCheckpointProjection(stream, firstProjection, nil); err != nil {
		t.Fatalf("queue first checkpoint: %v", err)
	}
	firstRequestIDs := checkpointPendingRequestIDs(stream)
	if len(firstRequestIDs) == 0 {
		t.Fatal("first projection has no pending Blob writes")
	}

	secondProjection := checkpointProjectionWithAdditionalBlob(t, firstProjection, "latest checkpoint state")
	if err := service.queueCheckpointProjection(stream, secondProjection, nil); err != nil {
		t.Fatalf("queue second checkpoint: %v", err)
	}
	secondRequestIDs := checkpointPendingRequestIDs(stream)
	if len(secondRequestIDs) != len(firstRequestIDs)+1 {
		t.Fatalf("pending Blob writes after coalesce = %d, want %d", len(secondRequestIDs), len(firstRequestIDs)+1)
	}
	if checkpointBlobWriteEventCount(t, service, stream) != len(secondRequestIDs) {
		t.Fatalf("Blob dispatches = %d, want %d without duplicate writes", checkpointBlobWriteEventCount(t, service, stream), len(secondRequestIDs))
	}

	if err := service.handleCheckpointBlobResult(stream, checkpointBlobAck(firstRequestIDs[0])); err != nil {
		t.Fatalf("late first-generation ACK: %v", err)
	}
	acknowledgeCheckpointBlobs(t, service, stream)

	checkpoints := checkpointEvents(t, service, stream)
	if len(checkpoints) != 1 {
		t.Fatalf("published checkpoints = %d, want 1", len(checkpoints))
	}
	if !checkpointHasLatestProjection(checkpoints[0].GetConversationCheckpointUpdate()) {
		t.Fatalf("published checkpoint is not the latest projection")
	}
}

func TestCheckpointBlobRuntimeEventsIncludeLifecycleMetrics(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	recorder := newDebugRecorderWithQueue(t.TempDir(), service.broker, testObservabilityConfig{enabled: true}, 64)
	defer recorder.Close()
	service.debug = recorder

	if err := service.queueCheckpointProjection(stream, projection, nil); err != nil {
		t.Fatalf("queue checkpoint: %v", err)
	}
	acknowledgeCheckpointBlobs(t, service, stream)
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatalf("flush runtime events: %v", err)
	}
	payload, err := os.ReadFile(debugFilePath(recorder.historyRoot, stream.ConversationID, "debug", "runtime.jsonl"))
	if err != nil {
		t.Fatalf("read runtime events: %v", err)
	}
	for _, event := range []string{"checkpoint_projection_queued", "checkpoint_blob_dispatched", "checkpoint_blob_acknowledged", "checkpoint_published"} {
		if !strings.Contains(string(payload), `"event":"`+event+`"`) {
			t.Fatalf("missing runtime event %q in %s", event, payload)
		}
	}
	for _, field := range []string{"request_id", "generation", "required_blob_count", "pending_blob_count", "new_blob_count", "total_blob_bytes", "duration_ms"} {
		if !strings.Contains(string(payload), `"`+field+`"`) {
			t.Fatalf("missing lifecycle metric %q in %s", field, payload)
		}
	}
}

func TestCheckpointBlobTimeoutRuntimeEventIncludesMissingBlobIDs(t *testing.T) {
	service, stream, projection := testCheckpointBlobProjection(t)
	recorder := newDebugRecorderWithQueue(t.TempDir(), service.broker, testObservabilityConfig{enabled: true}, 16)
	defer recorder.Close()
	service.debug = recorder

	if err := service.queueCheckpointProjection(stream, projection, nil); err != nil {
		t.Fatalf("queue checkpoint: %v", err)
	}
	if err := service.handleCheckpointBlobTimeout(stream); err != nil {
		t.Fatalf("checkpoint timeout: %v", err)
	}
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatalf("flush runtime events: %v", err)
	}
	payload, err := os.ReadFile(debugFilePath(recorder.historyRoot, stream.ConversationID, "debug", "runtime.jsonl"))
	if err != nil {
		t.Fatalf("read runtime events: %v", err)
	}
	if !strings.Contains(string(payload), `"event":"checkpoint_blob_timeout"`) || !strings.Contains(string(payload), `"missing_blob_ids"`) {
		t.Fatalf("timeout runtime event missing detail: %s", payload)
	}
}

func checkpointProjectionWithAdditionalBlob(t *testing.T, source *CheckpointProjection, summary string) *CheckpointProjection {
	t.Helper()
	state := proto.Clone(source.State).(*agentv1.ConversationStateStructure)
	state.Summary = []byte(summary)
	blobs := append([]CheckpointBlob(nil), source.Blobs...)
	blobs = append(blobs, CheckpointBlob{ID: []byte("latest-blob"), Data: []byte("latest blob data")})
	return &CheckpointProjection{State: state, Blobs: blobs}
}

func checkpointPendingRequestIDs(stream *ActiveStream) []uint32 {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	requestIDs := make([]uint32, 0, len(stream.PendingCheckpointBlobWrites))
	for requestID := range stream.PendingCheckpointBlobWrites {
		requestIDs = append(requestIDs, requestID)
	}
	return requestIDs
}

func checkpointBlobAck(requestID uint32) *agentv1.KvClientMessage {
	return &agentv1.KvClientMessage{
		Id: requestID,
		Message: &agentv1.KvClientMessage_SetBlobResult{
			SetBlobResult: &agentv1.SetBlobResult{},
		},
	}
}

func checkpointBlobWriteEventCount(t *testing.T, service *Service, stream *ActiveStream) int {
	t.Helper()
	count := 0
	for _, event := range readCheckpointTestEvents(t, service, stream) {
		if event.Message.GetKvServerMessage().GetSetBlobArgs() != nil {
			count++
		}
	}
	return count
}

func checkpointEvents(t *testing.T, service *Service, stream *ActiveStream) []*agentv1.AgentServerMessage {
	t.Helper()
	var checkpoints []*agentv1.AgentServerMessage
	for _, event := range readCheckpointTestEvents(t, service, stream) {
		if event.Message.GetConversationCheckpointUpdate() != nil {
			checkpoints = append(checkpoints, event.Message)
		}
	}
	return checkpoints
}

func checkpointHasLatestProjection(state *agentv1.ConversationStateStructure) bool {
	return state != nil && bytes.Equal(state.GetSummary(), []byte("latest checkpoint state"))
}

func testCheckpointBlobProjection(t *testing.T) (*Service, *ActiveStream, *CheckpointProjection) {
	t.Helper()
	broker := NewStreamBroker()
	service := &Service{
		store:     NewConversationFileStore(t.TempDir()),
		projector: NewHistoryProjector(),
		broker:    broker,
	}
	stream, err := broker.OpenStream(
		"request-1", "conversation-1", 1, "default", "default",
		agentv1.AgentMode_AGENT_MODE_AGENT, "hello",
	)
	if err != nil {
		t.Fatalf("OpenStream() error = %v", err)
	}
	conversation := &ConversationFile{
		ConversationID:        "conversation-1",
		RootConversationID:    "conversation-1",
		Mode:                  "agent",
		NextTurnSeq:           2,
		NextEntrySeq:          3,
		TokenDetailsMaxTokens: projectedConversationMaxTokens,
		Entries: []HistoryEntry{
			testCheckpointUserEntry(t),
			newAssistantTextEntry(1, "request-1", "hi", "", ""),
		},
	}
	projection, err := service.projector.ProjectCheckpointProjection(conversation)
	if err != nil {
		t.Fatalf("ProjectCheckpointProjection() error = %v", err)
	}
	if err := service.replaceCheckpointConversation(stream, conversation); err != nil {
		t.Fatalf("replaceCheckpointConversation() error = %v", err)
	}
	return service, stream, projection
}

func testCheckpointUserEntry(t *testing.T) HistoryEntry {
	t.Helper()
	payload, err := protojson.Marshal(&agentv1.UserMessage{Text: "hello", MessageId: "message-1"})
	if err != nil {
		t.Fatalf("marshal user message: %v", err)
	}
	return HistoryEntry{Seq: 1, TurnSeq: 1, RequestID: "request-1", Role: "user", Kind: "user_message", Payload: payload}
}

func acknowledgeCheckpointBlobs(t *testing.T, service *Service, stream *ActiveStream) {
	t.Helper()
	for {
		stream.mu.Lock()
		requestIDs := make([]uint32, 0, len(stream.PendingCheckpointBlobWrites))
		for requestID := range stream.PendingCheckpointBlobWrites {
			requestIDs = append(requestIDs, requestID)
		}
		stream.mu.Unlock()
		if len(requestIDs) == 0 {
			return
		}
		for _, requestID := range requestIDs {
			if err := service.handleCheckpointBlobResult(stream, &agentv1.KvClientMessage{
				Id: requestID,
				Message: &agentv1.KvClientMessage_SetBlobResult{
					SetBlobResult: &agentv1.SetBlobResult{},
				},
			}); err != nil {
				t.Fatalf("handleCheckpointBlobResult(%d) error = %v", requestID, err)
			}
		}
	}
}

func readCheckpointTestEvents(t *testing.T, service *Service, stream *ActiveStream) []StreamEvent {
	t.Helper()
	events, err := service.broker.ReadFromCursor(stream.RequestID, 0)
	if err != nil {
		t.Fatalf("ReadFromCursor() error = %v", err)
	}
	return events
}
