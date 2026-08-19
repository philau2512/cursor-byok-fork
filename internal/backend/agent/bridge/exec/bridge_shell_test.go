package execbridge

import (
	"strings"
	"testing"

	runtimecore "cursor/internal/backend/agent/core"
)

func TestEnrichCommandForStreaming(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "python script execution on windows powershell",
			input:    ".\\.venv\\Scripts\\python.exe manage.py test apps --no-input",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; .\\.venv\\Scripts\\python.exe manage.py test apps --no-input",
		},
		{
			name:     "pytest execution",
			input:    "pytest tests/test_api.py",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; pytest tests/test_api.py",
		},
		{
			name:     "arbitrary python script execution",
			input:    ".\\main.py --arg value",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; .\\main.py --arg value",
		},
		{
			name:     "short python file execution",
			input:    "m.py",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; m.py",
		},
		{
			name:     "uv run tool execution",
			input:    "uv run ruff check",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; uv run ruff check",
		},
		{
			name:     "python3 execution",
			input:    "python3 -m unittest",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; $env:PYTHONIOENCODING=\"utf-8\"; python3 -m unittest",
		},
		{
			name:     "node script execution",
			input:    "node server.js",
			expected: "$env:NODE_NO_WARNINGS=\"1\"; node server.js",
		},
		{
			name:     "plain git status command unchanged",
			input:    "git status",
			expected: "git status",
		},
		{
			name:     "already contains pythonunbuffered",
			input:    "$env:PYTHONUNBUFFERED=\"1\"; python test.py",
			expected: "$env:PYTHONUNBUFFERED=\"1\"; python test.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enrichCommandForStreaming(tt.input)
			if got != tt.expected {
				t.Errorf("enrichCommandForStreaming(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestOpenShellEnrichesCommand(t *testing.T) {
	bridge := NewBridge()
	toolCall := runtimecore.ToolInvocation{
		CallID:   "call-shell-1",
		ToolName: "Shell",
		ArgsJSON: []byte(`{"command": ".\\.venv\\Scripts\\python.exe manage.py test"}`),
	}

	msg, pending, err := bridge.OpenExec(OpenExecContext{}, toolCall)
	if err != nil {
		t.Fatalf("OpenExec() error = %v", err)
	}

	shellArgs := msg.GetExecServerMessage().GetShellStreamArgs()
	if shellArgs == nil {
		t.Fatal("expected ShellStreamArgs message")
	}

	if !strings.Contains(shellArgs.GetCommand(), "PYTHONUNBUFFERED") {
		t.Fatalf("expected enriched command with PYTHONUNBUFFERED, got %q", shellArgs.GetCommand())
	}
	if pending.ExecKind != "shell" {
		t.Fatalf("pending ExecKind = %q, want shell", pending.ExecKind)
	}
}

func TestDefaultShellTimeout(t *testing.T) {
	if timeout := defaultShellTimeout(shellResultArgs{Command: "python test.py"}); timeout != 30000 {
		t.Fatalf("defaultShellTimeout() = %d, want 30000", timeout)
	}
}
