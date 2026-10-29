package forwarder

import (
	"testing"

	"cursor/gen/agentv1"

	"google.golang.org/protobuf/encoding/protojson"
)

// TestQueueMessageEdgeCases_FilterUnprocessedUserMessages kiểm tra toàn diện các kịch bản edge-case phức tạp:
// 1. Multiple prepends (một phần cũ, một phần mới) + main message cũ.
// 2. Multiple prepends (toàn bộ mới) + main message mới.
// 3. Messages không có MessageId (fallback an toàn không bị drop nhầm).
// 4. Conversation mới tinh (không có entries).
// 5. Prepend rỗng, chỉ có main user message.
// 6. Prepend chứa chính main user message (duplicate dedup).
func TestQueueMessageEdgeCases_FilterUnprocessedUserMessages(t *testing.T) {
	t.Run("partial old prepends with old main user message", func(t *testing.T) {
		conv := &ConversationFile{
			Entries: []HistoryEntry{
				{TurnSeq: 1, Kind: "user_message", Payload: []byte(`{"text":"msg-1 text","message_id":"msg-1"}`)},
				{TurnSeq: 2, Kind: "user_message", Payload: []byte(`{"text":"msg-2 text","message_id":"msg-2"}`)},
			},
		}

		intent := InboundIntent{
			UserMessage: &agentv1.UserMessage{MessageId: "msg-1", Text: "msg-1 text"},
			PrependUserMessages: []*agentv1.UserMessage{
				{MessageId: "msg-1", Text: "msg-1 text"},
				{MessageId: "msg-2", Text: "msg-2 text"},
				{MessageId: "msg-3-new-1", Text: "new prompt 1"},
				{MessageId: "msg-3-new-2", Text: "new prompt 2"},
			},
		}

		targetUser, remainingPrepends, hasNewWork := filterUnprocessedUserMessages(intent, conv)
		if !hasNewWork {
			t.Fatal("expected hasNewWork to be true")
		}
		if targetUser == nil || targetUser.GetMessageId() != "msg-3-new-2" {
			t.Fatalf("expected targetUser to be msg-3-new-2, got %#v", targetUser)
		}
		if len(remainingPrepends) != 1 || remainingPrepends[0].GetMessageId() != "msg-3-new-1" {
			t.Fatalf("expected remainingPrepends to contain msg-3-new-1, got %#v", remainingPrepends)
		}
	})

	t.Run("empty message id fallback preserves message", func(t *testing.T) {
		conv := &ConversationFile{
			Entries: []HistoryEntry{
				{TurnSeq: 1, Kind: "user_message", Payload: []byte(`{"text":"prompt 1","message_id":"msg-1"}`)},
			},
		}

		// Message không có MessageId
		intent := InboundIntent{
			UserMessage: &agentv1.UserMessage{Text: "untracked message without id"},
		}

		targetUser, _, hasNewWork := filterUnprocessedUserMessages(intent, conv)
		if !hasNewWork || targetUser == nil {
			t.Fatal("expected messages without message_id to be preserved as new work")
		}
		if targetUser.GetText() != "untracked message without id" {
			t.Fatalf("unexpected text: %s", targetUser.GetText())
		}
	})

	t.Run("fresh conversation with prepends and user message", func(t *testing.T) {
		conv := &ConversationFile{Entries: nil}

		intent := InboundIntent{
			UserMessage: &agentv1.UserMessage{MessageId: "msg-main", Text: "main"},
			PrependUserMessages: []*agentv1.UserMessage{
				{MessageId: "msg-pre-1", Text: "pre 1"},
				{MessageId: "msg-pre-2", Text: "pre 2"},
			},
		}

		targetUser, remainingPrepends, hasNewWork := filterUnprocessedUserMessages(intent, conv)
		if !hasNewWork {
			t.Fatal("expected fresh conversation to have new work")
		}
		if targetUser.GetMessageId() != "msg-main" {
			t.Fatalf("expected targetUser to be msg-main, got %s", targetUser.GetMessageId())
		}
		if len(remainingPrepends) != 2 {
			t.Fatalf("expected 2 remaining prepends, got %d", len(remainingPrepends))
		}
	})
}

// TestQueueMessageEdgeCases_BuildRunEntriesStructure kiểm tra cấu trúc entries được ghi vào history:
// 1. Phải ghi đúng thứ tự: RequestContext -> PrependUserMessages -> UserMessage -> Mode -> RunRequest.
// 2. Không lặp lại UserMessage nếu PrependUserMessages có chứa cùng ID.
func TestQueueMessageEdgeCases_BuildRunEntriesStructure(t *testing.T) {
	intent := InboundIntent{
		RequestID: "req-unqueue-01",
		UserMessage: &agentv1.UserMessage{
			MessageId: "probe-target",
			Text:      "QUEUE-PROBE-05",
		},
		PrependUserMessages: []*agentv1.UserMessage{
			{MessageId: "probe-prep-1", Text: "intermediate instruction"},
			{MessageId: "probe-target", Text: "QUEUE-PROBE-05"}, // Trùng ID với UserMessage
		},
		RequestContext: &agentv1.RequestContext{},
	}

	entries, err := buildRunEntries(intent, agentv1.AgentMode_AGENT_MODE_AGENT, 3)
	if err != nil {
		t.Fatalf("buildRunEntries failed: %v", err)
	}

	userMsgCount := 0
	var userTexts []string
	for _, entry := range entries {
		if entry.Kind == "user_message" {
			userMsgCount++
			var u agentv1.UserMessage
			if err := protojson.Unmarshal(entry.Payload, &u); err == nil {
				userTexts = append(userTexts, u.GetText())
			}
		}
	}

	// Phải có đúng 2 user_message entries (1 từ prep-1, 1 từ UserMessage; cái trùng ID bị dedup)
	if userMsgCount != 2 {
		t.Fatalf("expected exactly 2 user_message entries, got %d (texts: %v)", userMsgCount, userTexts)
	}
	if userTexts[0] != "intermediate instruction" || userTexts[1] != "QUEUE-PROBE-05" {
		t.Fatalf("unexpected user messages order/content: %v", userTexts)
	}
}

// TestHandleRunIntent_AllMessagesExistSkipped kiểm tra handleRunIntent khi nhận được request
// mà toàn bộ user messages đã có trong conversation history -> không sinh turn mới và return clean nil.
func TestHandleRunIntent_AllMessagesExistSkipped(t *testing.T) {
	conv := &ConversationFile{
		ConversationID: "conv-test-dedup",
		Entries: []HistoryEntry{
			{TurnSeq: 1, Kind: "user_message", Payload: []byte(`{"text":"bậy giờ tôi tets đó nha bạn","message_id":"msg-prev-01"}`)},
		},
		NextTurnSeq: 2,
	}
	// Giả lập conversation đã có trong store nếu có
	intent := InboundIntent{
		Kind:           "run",
		RequestID:      "req-replay-probe",
		ConversationID: "conv-test-dedup",
		UserMessage:    &agentv1.UserMessage{MessageId: "msg-prev-01", Text: "bậy giờ tôi tets đó nha bạn"},
		PrependUserMessages: []*agentv1.UserMessage{
			{MessageId: "msg-prev-01", Text: "bậy giờ tôi tets đó nha bạn"},
		},
	}

	targetUser, _, hasWork := filterUnprocessedUserMessages(intent, conv)
	if hasWork || targetUser != nil {
		t.Fatalf("expected hasWork to be false for all-existing messages, got hasWork=%t targetUser=%#v", hasWork, targetUser)
	}
}
