package execbridge

import (
	"path/filepath"
	"testing"
)

func TestDecodeGrepToolArgsWindowsOptimization(t *testing.T) {
	tests := []struct {
		name         string
		argsJSON     []byte
		wantPattern  string
		wantGlob     *string
		wantPath     *string
		wantOutMode  *string
		wantCaseIns  *bool
	}{
		{
			name: "normalizes backslashes in glob to forward slashes",
			argsJSON: []byte(`{
				"pattern": "func main",
				"glob": "internal\\backend\\*.go"
			}`),
			wantPattern: "func main",
			wantGlob:    stringPtr("internal/backend/*.go"),
			wantPath:    nil,
		},
		{
			name: "normalizes trailing backslash in path",
			argsJSON: []byte(`{
				"pattern": "type Service struct",
				"path": "C:\\workspace\\cursor-byok\\",
				"glob": "**\\*.go",
				"-i": true,
				"output_mode": "content"
			}`),
			wantPattern: "type Service struct",
			wantPath:    stringPtr(filepath.Clean("C:\\workspace\\cursor-byok")),
			wantGlob:    stringPtr("**/*.go"),
			wantOutMode: stringPtr("content"),
			wantCaseIns: boolPtr(true),
		},
		{
			name: "preserves forward slashes glob and path",
			argsJSON: []byte(`{
				"pattern": "import",
				"path": "src/pkg",
				"glob": "src/**/*.ts"
			}`),
			wantPattern: "import",
			wantPath:    stringPtr(filepath.Clean("src/pkg")),
			wantGlob:    stringPtr("src/**/*.ts"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeGrepToolArgs(tt.argsJSON, "call-grep-1")
			if err != nil {
				t.Fatalf("DecodeGrepToolArgs() error = %v", err)
			}
			if got.Pattern != tt.wantPattern {
				t.Errorf("Pattern = %q, want %q", got.Pattern, tt.wantPattern)
			}
			if tt.wantGlob == nil && got.Glob != nil {
				t.Errorf("Glob = %v, want nil", *got.Glob)
			} else if tt.wantGlob != nil && (got.Glob == nil || *got.Glob != *tt.wantGlob) {
				t.Errorf("Glob = %v, want %v", got.Glob, *tt.wantGlob)
			}
			if tt.wantPath == nil && got.Path != nil {
				t.Errorf("Path = %v, want nil", *got.Path)
			} else if tt.wantPath != nil && (got.Path == nil || *got.Path != *tt.wantPath) {
				t.Errorf("Path = %v, want %v", got.Path, *tt.wantPath)
			}
			if tt.wantOutMode != nil && (got.OutputMode == nil || *got.OutputMode != *tt.wantOutMode) {
				t.Errorf("OutputMode = %v, want %v", got.OutputMode, *tt.wantOutMode)
			}
			if tt.wantCaseIns != nil && (got.CaseInsensitive == nil || *got.CaseInsensitive != *tt.wantCaseIns) {
				t.Errorf("CaseInsensitive = %v, want %v", got.CaseInsensitive, *tt.wantCaseIns)
			}
		})
	}
}

func boolPtr(v bool) *bool {
	return &v
}
