package forwarder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cursor/gen/agentv1"
	runtimecore "cursor/internal/backend/agent/core"
)

func int64Pointer(value int64) *int64 {
	return &value
}

func TestAwaitShellSnapshotReturnsOnlyUnreadOutput(t *testing.T) {
	service, stream, _ := testShellRecoveryFixture(t, "opened")
	stream.mu.Lock()
	stream.BackgroundShells["42"] = &BackgroundShellState{
		ShellID:      "42",
		Status:       backgroundShellStatusRunning,
		StdoutBuffer: "first stdout\n",
		StderrBuffer: "first stderr\n",
		CreatedAt:    time.Now().UTC().Add(-time.Second),
	}
	stream.mu.Unlock()

	first := service.awaitShellSnapshot(stream, awaitShellArgs{ShellID: "42", BlockUntilMS: int64Pointer(0)})
	if first.Stdout != "first stdout\n" || first.Stderr != "first stderr\n" {
		t.Fatalf("first AwaitShell output = stdout %q stderr %q", first.Stdout, first.Stderr)
	}

	stream.mu.Lock()
	state := stream.BackgroundShells["42"]
	state.StdoutBuffer += "second stdout\n"
	state.StderrBuffer += "second stderr\n"
	stream.mu.Unlock()

	second := service.awaitShellSnapshot(stream, awaitShellArgs{ShellID: "42", BlockUntilMS: int64Pointer(0)})
	if second.Stdout != "second stdout\n" || second.Stderr != "second stderr\n" {
		t.Fatalf("second AwaitShell output = stdout %q stderr %q, want only new output", second.Stdout, second.Stderr)
	}
}

func TestAwaitShellSnapshotHandlesPatternsAndStates(t *testing.T) {
	testCases := []struct {
		name         string
		shellID      string
		status       string
		pattern      string
		stdout       string
		wantStatus   string
		wantMatched  bool
		wantTimeout  bool
		wantError    string
		blockUntilMS int64
	}{
		{name: "regex match", shellID: "match", status: backgroundShellStatusRunning, pattern: `ready\s+now`, stdout: "ready now", wantStatus: backgroundShellStatusRunning, wantMatched: true},
		{name: "invalid regex", shellID: "invalid", status: backgroundShellStatusRunning, pattern: `[`, wantStatus: backgroundShellStatusRunning, wantTimeout: true, wantError: "invalid AwaitShell pattern", blockUntilMS: 1},
		{name: "unknown shell", shellID: "missing", wantStatus: backgroundShellStatusUnknown},
		{name: "backgrounded shell", shellID: "backgrounded", status: backgroundShellStatusBackgrounded, wantStatus: backgroundShellStatusBackgrounded, wantTimeout: true, blockUntilMS: 1},
		{name: "completed shell", shellID: "completed", status: backgroundShellStatusCompleted, wantStatus: backgroundShellStatusCompleted},
		{name: "rejected shell", shellID: "rejected", status: backgroundShellStatusRejected, wantStatus: backgroundShellStatusRejected},
		{name: "permission denied shell", shellID: "permission", status: backgroundShellStatusPermissionDenied, wantStatus: backgroundShellStatusPermissionDenied},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service, stream, _ := testShellRecoveryFixture(t, "opened")
			if testCase.status != "" {
				stream.mu.Lock()
				stream.BackgroundShells[testCase.shellID] = &BackgroundShellState{
					ShellID:      testCase.shellID,
					Status:       testCase.status,
					StdoutBuffer: testCase.stdout,
					CreatedAt:    time.Now().UTC().Add(time.Second),
				}
				stream.mu.Unlock()
			}

			result := service.awaitShellSnapshot(stream, awaitShellArgs{ShellID: testCase.shellID, Pattern: testCase.pattern, BlockUntilMS: int64Pointer(testCase.blockUntilMS)})
			if result.Status != testCase.wantStatus || result.Matched != testCase.wantMatched || result.TimedOut != testCase.wantTimeout {
				t.Fatalf("AwaitShell result = %#v, want status=%q matched=%t timed_out=%t", result, testCase.wantStatus, testCase.wantMatched, testCase.wantTimeout)
			}
			if testCase.wantError != "" && !strings.Contains(result.Message, testCase.wantError) {
				t.Fatalf("AwaitShell message = %q, want %q", result.Message, testCase.wantError)
			}
		})
	}
}

func TestAwaitShellSnapshotUsesActualTimeout(t *testing.T) {
	service, stream, _ := testShellRecoveryFixture(t, "opened")
	stream.mu.Lock()
	stream.BackgroundShells["slow"] = &BackgroundShellState{
		ShellID:   "slow",
		Status:    backgroundShellStatusRunning,
		CreatedAt: time.Now().UTC().Add(-time.Second),
	}
	stream.mu.Unlock()

	started := time.Now()
	result := service.awaitShellSnapshot(stream, awaitShellArgs{ShellID: "slow", BlockUntilMS: int64Pointer(10)})
	elapsed := time.Since(started)
	if !result.TimedOut {
		t.Fatalf("AwaitShell timed_out = false, want true")
	}
	if elapsed < 10*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("AwaitShell elapsed = %s, want approximately 10ms", elapsed)
	}
}

func TestWriteShellStdinTerminalErrorCompletesToolOnce(t *testing.T) {
	service, stream, _ := testShellRecoveryFixture(t, "opened")
	pending := initializePendingExecForTracking(runtimecore.PendingExec{
		MessageID:   91,
		ExecID:      "exec-write-stdin-91",
		ExecKind:    "write_shell_stdin",
		ToolCallID:  "tool-write-stdin-91",
		ModelCallID: "model-call-91",
		ArgsJSON:    []byte(`{"shell_id":42,"chars":"exit\\n"}`),
		OpenedAt:    time.Now().UTC(),
		StreamState: "opened",
	})
	stream.mu.Lock()
	stream.BackgroundShells["42"] = &BackgroundShellState{ShellID: "42", Status: backgroundShellStatusCompleted, CreatedAt: time.Now().UTC().Add(-time.Second)}
	stream.PendingExecs = map[string]runtimecore.PendingExec{pending.ExecID: pending}
	stream.mu.Unlock()
	terminalError := &agentv1.ExecClientMessage{
		Id:     pending.MessageID,
		ExecId: pending.ExecID,
		Message: &agentv1.ExecClientMessage_WriteShellStdinResult{
			WriteShellStdinResult: &agentv1.WriteShellStdinResult{
				Result: &agentv1.WriteShellStdinResult_Error{Error: &agentv1.WriteShellStdinError{Error: "shell 42 has already exited"}},
			},
		},
	}

	if err := service.handleExecResult(InboundIntent{RequestID: stream.RequestID, ExecClientMessage: terminalError}); err != nil {
		t.Fatalf("terminal stdin error = %v", err)
	}
	if _, found := snapshotPendingExec(stream, pending.ExecID); found {
		t.Fatal("terminal stdin error left the tool pending")
	}
	if completions := shellCompletionCount(stream, pending.ToolCallID); completions != 1 {
		t.Fatalf("completion count = %d, want 1", completions)
	}
	if !shellHistoryContains(stream, "shell 42 has already exited") {
		t.Fatal("terminal stdin error was not persisted")
	}

	if err := service.handleExecResult(InboundIntent{RequestID: stream.RequestID, ExecClientMessage: terminalError}); err != nil {
		t.Fatalf("duplicate terminal stdin error = %v, want ignored", err)
	}
	if completions := shellCompletionCount(stream, pending.ToolCallID); completions != 1 {
		t.Fatalf("duplicate terminal stdin error emitted %d completions, want 1", completions)
	}
}

func TestForceBackgroundShellDuplicateResultCompletesOnce(t *testing.T) {
	service, stream, _ := testShellRecoveryFixture(t, "opened")
	pending := initializePendingExecForTracking(runtimecore.PendingExec{
		MessageID:   92,
		ExecID:      "exec-force-background-92",
		ExecKind:    "force_background_shell",
		ToolCallID:  "tool-force-background-92",
		ModelCallID: "model-call-92",
		ArgsJSON:    []byte(`{"tool_call_id":"tool-shell-42"}`),
		OpenedAt:    time.Now().UTC(),
		StreamState: "opened",
	})
	stream.mu.Lock()
	stream.PendingExecs = map[string]runtimecore.PendingExec{pending.ExecID: pending}
	stream.mu.Unlock()
	result := &agentv1.ExecClientMessage{
		Id:     pending.MessageID,
		ExecId: pending.ExecID,
		Message: &agentv1.ExecClientMessage_ForceBackgroundShellResult{
			ForceBackgroundShellResult: &agentv1.ForceBackgroundShellResult{
				ShellResult: &agentv1.ShellResult{
					Result: &agentv1.ShellResult_Success{Success: &agentv1.ShellSuccess{}},
				},
			},
		},
	}

	if err := service.handleExecResult(InboundIntent{RequestID: stream.RequestID, ExecClientMessage: result}); err != nil {
		t.Fatalf("force background result = %v", err)
	}
	if _, found := snapshotPendingExec(stream, pending.ExecID); found {
		t.Fatal("force background result left the tool pending")
	}
	if completions := shellCompletionCount(stream, pending.ToolCallID); completions != 1 {
		t.Fatalf("completion count = %d, want 1", completions)
	}

	if err := service.handleExecResult(InboundIntent{RequestID: stream.RequestID, ExecClientMessage: result}); err != nil {
		t.Fatalf("duplicate force background result = %v, want ignored", err)
	}
	if completions := shellCompletionCount(stream, pending.ToolCallID); completions != 1 {
		t.Fatalf("duplicate force background result emitted %d completions, want 1", completions)
	}
}

func TestDecodeAwaitShellArgsAcceptsNumericAndStringShellID(t *testing.T) {
	testCases := []struct {
		name   string
		raw    string
		wantID string
		wantMS int64
	}{
		{name: "number id", raw: `{"shell_id":4,"block_until_ms":30000}`, wantID: "4", wantMS: 30000},
		{name: "string id", raw: `{"shell_id":"4","block_until_ms":12000}`, wantID: "4", wantMS: 12000},
		{name: "camel case shellId", raw: `{"shellId":7}`, wantID: "7", wantMS: 0},
		{name: "task_id fallback", raw: `{"task_id":42}`, wantID: "42", wantMS: 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args, err := decodeAwaitShellArgs([]byte(tc.raw))
			if err != nil {
				t.Fatalf("decodeAwaitShellArgs() error = %v", err)
			}
			if args.ShellID != tc.wantID {
				t.Fatalf("args.ShellID = %q, want %q", args.ShellID, tc.wantID)
			}
			if tc.wantMS > 0 && (args.BlockUntilMS == nil || *args.BlockUntilMS != tc.wantMS) {
				t.Fatalf("args.BlockUntilMS = %v, want %d", args.BlockUntilMS, tc.wantMS)
			}
		})
	}
}

func TestRefreshBackgroundShellFromTerminalFileWithLastExitCodeAndStatus(t *testing.T) {
	tempDir := t.TempDir()
	terminalContent := `---
pid: 28948
cwd: C:\workspace
last_command: gradle compileJava
last_exit_code: 0
status: succeeded
---
BUILD SUCCESSFUL in 38s
1 actionable task: 1 executed
`
	if err := os.WriteFile(filepath.Join(tempDir, "4.txt"), []byte(terminalContent), 0o644); err != nil {
		t.Fatalf("failed to write mock terminal file: %v", err)
	}

	service, stream, _ := testShellRecoveryFixture(t, "opened")
	stream.mu.Lock()
	stream.TerminalsFolder = tempDir
	stream.mu.Unlock()

	result := service.awaitShellSnapshot(stream, awaitShellArgs{ShellID: "4", BlockUntilMS: int64Pointer(30000)})
	if result.Status != backgroundShellStatusCompleted {
		t.Fatalf("result.Status = %q, want %q", result.Status, backgroundShellStatusCompleted)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("result.ExitCode = %v, want 0", result.ExitCode)
	}
	if result.TimedOut {
		t.Fatal("result.TimedOut = true, want false (should complete immediately)")
	}
	if !strings.Contains(result.Stdout, "BUILD SUCCESSFUL in 38s") {
		t.Fatalf("result.Stdout = %q, want containing build output", result.Stdout)
	}
}
