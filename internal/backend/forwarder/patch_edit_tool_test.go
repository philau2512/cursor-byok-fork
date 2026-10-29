package forwarder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePatchEditArgsNormalizesLineEndings(t *testing.T) {
	rawJSON := []byte(`{
		"path": "C:\\workspace\\test.txt",
		"old_string": "line1\r\nline2\r\n",
		"new_string": "new1\r\nnew2\r\n",
		"replace_all": false
	}`)

	args, err := decodePatchEditArgs(rawJSON)
	if err != nil {
		t.Fatalf("decodePatchEditArgs failed: %v", err)
	}

	if strings.Contains(args.OldString, "\r") {
		t.Fatalf("expected OldString to be normalized to LF, got: %q", args.OldString)
	}
	if want := "line1\nline2\n"; args.OldString != want {
		t.Fatalf("OldString = %q, want %q", args.OldString, want)
	}

	if strings.Contains(args.NewString, "\r") {
		t.Fatalf("expected NewString to be normalized to LF, got: %q", args.NewString)
	}
	if want := "new1\nnew2\n"; args.NewString != want {
		t.Fatalf("NewString = %q, want %q", args.NewString, want)
	}
}

func TestPatchEditArgsUnmarshalJSONNormalizesLineEndings(t *testing.T) {
	rawJSON := []byte(`{
		"path": "C:\\workspace\\test.txt",
		"old_string": "first\r\nsecond\r",
		"new_string": "replacement\r\n"
	}`)

	var args patchEditArgs
	if err := json.Unmarshal(rawJSON, &args); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	if want := "first\nsecond\n"; args.OldString != want {
		t.Fatalf("OldString = %q, want %q", args.OldString, want)
	}
	if want := "replacement\n"; args.NewString != want {
		t.Fatalf("NewString = %q, want %q", args.NewString, want)
	}
}

func TestComputePatchEditHandlesCRLFAndLFEquivalently(t *testing.T) {
	tests := []struct {
		name          string
		beforeContent string
		oldString     string
		newString     string
		replaceAll    bool
		wantAfter     string
		wantErr       bool
	}{
		{
			name:          "before CRLF and old_string LF",
			beforeContent: "hello\r\nworld\r\nfoo\r\n",
			oldString:     "world\n",
			newString:     "everyone\n",
			replaceAll:    false,
			wantAfter:     "hello\neveryone\nfoo\n",
		},
		{
			name:          "before LF and old_string CRLF",
			beforeContent: "hello\nworld\nfoo\n",
			oldString:     "world\r\n",
			newString:     "everyone\n",
			replaceAll:    false,
			wantAfter:     "hello\neveryone\nfoo\n",
		},
		{
			name:          "both CRLF",
			beforeContent: "line1\r\nline2\r\nline3\r\n",
			oldString:     "line2\r\n",
			newString:     "modified2\r\n",
			replaceAll:    false,
			wantAfter:     "line1\nmodified2\nline3\n",
		},
		{
			name:          "multi-line match across CRLF",
			beforeContent: "start\r\nblock_a\r\nblock_b\r\nend\r\n",
			oldString:     "block_a\nblock_b\n",
			newString:     "replaced_block\n",
			replaceAll:    false,
			wantAfter:     "start\nreplaced_block\nend\n",
		},
		{
			name:          "replace_all with CRLF old and new",
			beforeContent: "item\r\nitem\r\nitem\r\n",
			oldString:     "item\r\n",
			newString:     "done\r\n",
			replaceAll:    true,
			wantAfter:     "done\ndone\ndone\n",
		},
		{
			name:          "old_string not found",
			beforeContent: "hello\r\nworld\r\n",
			oldString:     "not_there\r\n",
			newString:     "foo\n",
			wantErr:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := pendingPatchEditPayload{
				ToolName:     patchEditToolName,
				ResolvedPath: `C:\workspace\test.txt`,
				Args: patchEditArgs{
					Path:       `C:\workspace\test.txt`,
					OldString:  tc.oldString,
					NewString:  tc.newString,
					ReplaceAll: tc.replaceAll,
				},
			}

			result, err := computePatchEdit(payload, tc.beforeContent)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("computePatchEdit failed: %v", err)
			}
			if result.AfterContent != tc.wantAfter {
				t.Fatalf("AfterContent = %q, want %q", result.AfterContent, tc.wantAfter)
			}
			if strings.Contains(result.AfterContent, "\r") {
				t.Fatalf("AfterContent contains CR carriage return: %q", result.AfterContent)
			}
		})
	}
}
