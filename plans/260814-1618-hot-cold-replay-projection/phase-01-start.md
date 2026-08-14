---
phase: 1
title: "Establish Lossless Baseline"
status: completed
priority: P1
effort: "0.5-1d"
dependencies: []
---

# Phase 1: Establish Lossless Baseline

## Overview

Define an exact replay-equivalence contract and capture a reproducible baseline for short and long conversations. The ~300k compaction policy remains unchanged.

## Requirements

- Functional: feature work must preserve provider-visible messages, including raw historical tool results and reasoning metadata.
- Non-functional: diagnostics must not retain provider request bodies or secrets.

## Related Code Files

- Modify: `internal/backend/forwarder/projector.go`
- Modify: `internal/backend/forwarder/compiler.go`
- Modify: `internal/backend/forwarder/*_test.go`

## Implementation Steps

1. Create representative replay fixtures: long coding conversation, multi-pass tool loop, completed Shell/Read/Edit work, and historical reasoning/tool signatures.
2. Add a canonical projection snapshot helper that compares role, content, content parts, tool-call IDs/arguments, and reasoning/provider metadata.
3. Establish feature-off byte/content-equivalence tests around `ProjectPromptReplay()` and `DefaultPromptCompiler.Compile()`.
4. Record baseline compile duration and estimated prompt-token breakdown without modifying the generated messages.
5. Ensure manual and auto compaction tests remain scoped to the current ~300k behavior.

## Success Criteria

- [ ] Fixture replay is stable and fully contains raw historical evidence.
- [ ] Any future optimization fails tests if it changes messages or ordering.
- [ ] Baseline separates compiler time from provider time.

## Risk Assessment

Snapshot tests must compare semantic provider message fields rather than unstable timestamps or debug identifiers.