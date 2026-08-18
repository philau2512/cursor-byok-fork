package forwarder

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"cursor/gen/agentv1"
)

const checkpointBlobWriteTimeout = 5 * time.Second

type pendingCheckpointBlobWrite struct {
	Key        string
	Generation uint64
	Dispatched time.Time
}

func successfulCheckpointTerminalAction(completion *pendingTurnCompletion) checkpointTerminalAction {
	if completion == nil {
		return checkpointTerminalAction{}
	}
	return checkpointTerminalAction{
		Kind:       checkpointTerminalActionComplete,
		Completion: *completion,
	}
}

func failedCheckpointTerminalAction(errorCode string, errorMessage string) checkpointTerminalAction {
	return checkpointTerminalAction{
		Kind:         checkpointTerminalActionFail,
		ErrorCode:    strings.TrimSpace(errorCode),
		ErrorMessage: strings.TrimSpace(errorMessage),
	}
}

func (service *Service) queueCheckpointProjection(stream *ActiveStream, projection *CheckpointProjection, completion *pendingTurnCompletion) error {
	return service.queueCheckpointProjectionWithTerminal(stream, projection, successfulCheckpointTerminalAction(completion))
}

func (service *Service) queueCheckpointProjectionWithTerminal(stream *ActiveStream, projection *CheckpointProjection, terminal checkpointTerminalAction) error {
	if service == nil || stream == nil || projection == nil || projection.State == nil {
		return nil
	}
	startedAt := time.Now().UTC()
	state, ok := proto.Clone(projection.State).(*agentv1.ConversationStateStructure)
	if !ok || state == nil {
		return fmt.Errorf("clone checkpoint state")
	}

	stream.mu.Lock()
	if stream.PendingCheckpointBlobWrites == nil {
		stream.PendingCheckpointBlobWrites = make(map[uint32]pendingCheckpointBlobWrite)
	}
	if stream.ConfirmedCheckpointBlobs == nil {
		stream.ConfirmedCheckpointBlobs = make(map[string]struct{})
	}
	stream.CheckpointGeneration++
	if stream.CheckpointGeneration == 0 {
		stream.CheckpointGeneration++
	}
	generation := stream.CheckpointGeneration
	if terminal.Kind == checkpointTerminalActionNone && stream.PendingCheckpoint != nil {
		terminal = stream.PendingCheckpoint.Terminal
	}
	required := make(map[string]struct{}, len(projection.Blobs))
	pendingKeys := make(map[string]struct{}, len(stream.PendingCheckpointBlobWrites))
	for _, write := range stream.PendingCheckpointBlobWrites {
		pendingKeys[write.Key] = struct{}{}
	}
	toWrite := make([]pendingCheckpointBlobWriteDispatch, 0, len(projection.Blobs))
	totalBytes := 0
	for _, blob := range projection.Blobs {
		key := string(blob.ID)
		if key == "" {
			continue
		}
		required[key] = struct{}{}
		totalBytes += len(blob.Data)
		if _, confirmed := stream.ConfirmedCheckpointBlobs[key]; confirmed {
			continue
		}
		if _, pending := pendingKeys[key]; pending {
			continue
		}
		stream.NextCheckpointBlobRequestID++
		if stream.NextCheckpointBlobRequestID == 0 {
			stream.NextCheckpointBlobRequestID++
		}
		requestID := stream.NextCheckpointBlobRequestID
		write := pendingCheckpointBlobWrite{Key: key, Generation: generation, Dispatched: startedAt}
		stream.PendingCheckpointBlobWrites[requestID] = write
		pendingKeys[key] = struct{}{}
		toWrite = append(toWrite, pendingCheckpointBlobWriteDispatch{requestID: requestID, blob: blob})
	}
	stream.PendingCheckpoint = &pendingCheckpointPublish{
		Generation:     generation,
		QueuedAt:       startedAt,
		TotalBlobBytes: totalBytes,
		NewBlobCount:   len(toWrite),
		State:          state,
		Required:       required,
		Terminal:       terminal,
	}
	if terminal.Kind != checkpointTerminalActionNone {
		stream.Phase = TurnPhaseCheckpointing
	}
	pendingCount := len(stream.PendingCheckpointBlobWrites)
	stream.UpdatedAt = startedAt
	stream.mu.Unlock()

	service.logCheckpointRuntime(stream, "checkpoint_projection_queued", map[string]any{
		"generation":          generation,
		"required_blob_count": len(required),
		"pending_blob_count":  pendingCount,
		"new_blob_count":      len(toWrite),
		"total_blob_bytes":    totalBytes,
		"duration_ms":         time.Since(startedAt).Milliseconds(),
	})
	for _, write := range toWrite {
		service.logCheckpointRuntime(stream, "checkpoint_blob_dispatched", map[string]any{
			"generation":          generation,
			"blob_request_id":     write.requestID,
			"blob_bytes":          len(write.blob.Data),
			"required_blob_count": len(required),
			"pending_blob_count":  pendingCount,
			"new_blob_count":      len(toWrite),
			"total_blob_bytes":    totalBytes,
			"duration_ms":         time.Since(startedAt).Milliseconds(),
		})
		if err := service.broker.Publish(stream.RequestID, StreamEvent{
			Message: buildSetCheckpointBlobMessage(write.requestID, write.blob),
		}); err != nil {
			return service.finishAfterCheckpointSyncFailure(stream, fmt.Errorf("publish checkpoint blob: %w", err))
		}
	}
	if service.checkpointProjectionReady(stream) {
		return service.publishReadyCheckpoint(stream)
	}
	service.scheduleCheckpointBlobTimeout(stream, generation)
	return nil
}

type pendingCheckpointBlobWriteDispatch struct {
	requestID uint32
	blob      CheckpointBlob
}

func (service *Service) scheduleCheckpointBlobTimeout(stream *ActiveStream, generation uint64) {
	if stream == nil || generation == 0 {
		return
	}
	service.scheduleStreamTimer(
		stream,
		providerTimerKey(streamTimerCheckpointBlobs, ""),
		checkpointBlobWriteTimeout,
		streamTimerCheckpointBlobs,
		"",
		0,
		fmt.Sprintf("checkpoint blob write timeout generation=%d", generation),
	)
}

func (service *Service) checkpointProjectionReady(stream *ActiveStream) bool {
	if stream == nil {
		return false
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.PendingCheckpoint == nil {
		return false
	}
	for key := range stream.PendingCheckpoint.Required {
		if _, confirmed := stream.ConfirmedCheckpointBlobs[key]; !confirmed {
			return false
		}
	}
	return true
}

func (service *Service) handleCheckpointBlobResult(stream *ActiveStream, message *agentv1.KvClientMessage) error {
	if service == nil || stream == nil || message == nil || message.GetSetBlobResult() == nil {
		return nil
	}
	now := time.Now().UTC()
	stream.mu.Lock()
	write, ok := stream.PendingCheckpointBlobWrites[message.GetId()]
	if ok {
		delete(stream.PendingCheckpointBlobWrites, message.GetId())
	}
	generation := uint64(0)
	required := false
	queuedAt := time.Time{}
	totalBytes := 0
	newBlobCount := 0
	if stream.PendingCheckpoint != nil {
		generation = stream.PendingCheckpoint.Generation
		queuedAt = stream.PendingCheckpoint.QueuedAt
		totalBytes = stream.PendingCheckpoint.TotalBlobBytes
		newBlobCount = stream.PendingCheckpoint.NewBlobCount
		if ok {
			_, required = stream.PendingCheckpoint.Required[write.Key]
		}
	}
	if ok && message.GetSetBlobResult().GetError() == nil {
		stream.ConfirmedCheckpointBlobs[write.Key] = struct{}{}
	}
	pendingCount := len(stream.PendingCheckpointBlobWrites)
	requiredCount := 0
	if stream.PendingCheckpoint != nil {
		requiredCount = len(stream.PendingCheckpoint.Required)
	}
	stream.UpdatedAt = now
	stream.mu.Unlock()
	if !ok {
		return nil
	}
	blobErr := message.GetSetBlobResult().GetError()
	eventName := "checkpoint_blob_acknowledged"
	if blobErr != nil {
		eventName = "checkpoint_blob_rejected"
	}
	service.logCheckpointRuntime(stream, eventName, map[string]any{
		"generation":          generation,
		"blob_generation":     write.Generation,
		"blob_request_id":     message.GetId(),
		"required_blob_count": requiredCount,
		"pending_blob_count":  pendingCount,
		"new_blob_count":      newBlobCount,
		"total_blob_bytes":    totalBytes,
		"duration_ms":         now.Sub(write.Dispatched).Milliseconds(),
		"checkpoint_age_ms":   now.Sub(queuedAt).Milliseconds(),
		"required_by_latest":  required,
	})
	if blobErr != nil && required {
		return service.finishAfterCheckpointSyncFailure(stream, fmt.Errorf(
			"client rejected checkpoint blob %s: %s",
			hex.EncodeToString([]byte(write.Key)),
			firstNonEmpty(strings.TrimSpace(blobErr.GetMessage()), "unknown error"),
		))
	}
	if service.checkpointProjectionReady(stream) {
		return service.publishReadyCheckpoint(stream)
	}
	return nil
}

func (service *Service) publishReadyCheckpoint(stream *ActiveStream) error {
	if service == nil || stream == nil {
		return nil
	}
	now := time.Now().UTC()
	stream.mu.Lock()
	pending := stream.PendingCheckpoint
	if pending == nil {
		stream.mu.Unlock()
		return nil
	}
	for key := range pending.Required {
		if _, confirmed := stream.ConfirmedCheckpointBlobs[key]; !confirmed {
			stream.mu.Unlock()
			return nil
		}
	}
	stream.PendingCheckpoint = nil
	state := pending.State
	terminal := pending.Terminal
	pendingCount := len(stream.PendingCheckpointBlobWrites)
	stream.UpdatedAt = now
	stream.mu.Unlock()
	clearStreamTimer(stream, providerTimerKey(streamTimerCheckpointBlobs, ""))
	service.logCheckpointRuntime(stream, "checkpoint_published", map[string]any{
		"generation":          pending.Generation,
		"required_blob_count": len(pending.Required),
		"pending_blob_count":  pendingCount,
		"new_blob_count":      pending.NewBlobCount,
		"total_blob_bytes":    pending.TotalBlobBytes,
		"duration_ms":         now.Sub(pending.QueuedAt).Milliseconds(),
	})
	if err := service.broker.Publish(stream.RequestID, StreamEvent{Message: buildCheckpointMessage(state)}); err != nil {
		if terminal.Kind != checkpointTerminalActionNone {
			log.Printf("forwarder checkpoint publish skipped before terminal request_id=%s err=%v", stream.RequestID, err)
			return service.finishCheckpointTerminalAction(stream, terminal)
		}
		return err
	}
	return service.finishCheckpointTerminalAction(stream, terminal)
}

func (service *Service) handleCheckpointBlobTimeout(stream *ActiveStream) error {
	if stream == nil {
		return nil
	}
	now := time.Now().UTC()
	stream.mu.Lock()
	pending := stream.PendingCheckpoint
	if pending == nil {
		stream.mu.Unlock()
		return nil
	}
	missing := make([]string, 0, len(pending.Required))
	for key := range pending.Required {
		if _, confirmed := stream.ConfirmedCheckpointBlobs[key]; !confirmed {
			missing = append(missing, hex.EncodeToString([]byte(key)))
		}
	}
	pendingCount := len(stream.PendingCheckpointBlobWrites)
	generation := pending.Generation
	queuedAt := pending.QueuedAt
	requiredCount := len(pending.Required)
	newBlobCount := pending.NewBlobCount
	totalBlobBytes := pending.TotalBlobBytes
	stream.mu.Unlock()
	if len(missing) == 0 {
		return service.publishReadyCheckpoint(stream)
	}
	service.logCheckpointRuntime(stream, "checkpoint_blob_timeout", map[string]any{
		"generation":          generation,
		"required_blob_count": requiredCount,
		"pending_blob_count":  pendingCount,
		"new_blob_count":      newBlobCount,
		"total_blob_bytes":    totalBlobBytes,
		"missing_blob_ids":    missing,
		"duration_ms":         now.Sub(queuedAt).Milliseconds(),
	})
	return service.finishAfterCheckpointSyncFailure(stream, fmt.Errorf("%d checkpoint blob writes timed out", len(missing)))
}

func (service *Service) finishAfterCheckpointSyncFailure(stream *ActiveStream, cause error) error {
	if stream == nil {
		return nil
	}
	stream.mu.Lock()
	pending := stream.PendingCheckpoint
	stream.PendingCheckpoint = nil
	stream.PendingCheckpointBlobWrites = make(map[uint32]pendingCheckpointBlobWrite)
	stream.UpdatedAt = time.Now().UTC()
	stream.mu.Unlock()
	clearStreamTimer(stream, providerTimerKey(streamTimerCheckpointBlobs, ""))
	if cause != nil {
		log.Printf("forwarder checkpoint blob sync skipped request_id=%s conversation_id=%s err=%v", stream.RequestID, stream.ConversationID, cause)
	}
	if pending != nil {
		return service.finishCheckpointTerminalAction(stream, pending.Terminal)
	}
	return nil
}

func (service *Service) finishCheckpointTerminalAction(stream *ActiveStream, terminal checkpointTerminalAction) error {
	switch terminal.Kind {
	case checkpointTerminalActionComplete:
		return service.finishSuccessfulTurnAfterCheckpoint(stream, terminal.Completion)
	case checkpointTerminalActionFail:
		return service.finishFailedTurnAfterCheckpoint(stream, terminal.ErrorCode, terminal.ErrorMessage)
	default:
		return nil
	}
}

func (service *Service) discardPendingCheckpoint(stream *ActiveStream, reason string) {
	if stream == nil {
		return
	}
	stream.mu.Lock()
	stream.PendingCheckpoint = nil
	stream.PendingCheckpointBlobWrites = make(map[uint32]pendingCheckpointBlobWrite)
	stream.UpdatedAt = time.Now().UTC()
	stream.mu.Unlock()
	clearStreamTimer(stream, providerTimerKey(streamTimerCheckpointBlobs, ""))
	if strings.TrimSpace(reason) != "" {
		log.Printf("forwarder pending checkpoint discarded request_id=%s conversation_id=%s reason=%s", stream.RequestID, stream.ConversationID, strings.TrimSpace(reason))
	}
}

func (service *Service) logCheckpointRuntime(stream *ActiveStream, eventName string, fields map[string]any) {
	if service == nil || service.debug == nil || stream == nil {
		return
	}
	service.debug.LogRuntime(context.Background(), stream.RequestID, stream.ConversationID, eventName, fields)
}
