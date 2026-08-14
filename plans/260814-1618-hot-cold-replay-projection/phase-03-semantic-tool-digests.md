---
phase: 3
title: "Lossless Execution-Path Optimizations"
status: pending
priority: P1
effort: "1-1.5d"
dependencies: [2]
---

# Phase 3: Lossless Execution-Path Optimizations

## Overview

Implement only the optimization selected by measured evidence, while retaining exact provider-visible history. The anticipated candidates optimize local work, cache stability, or unnecessary provider passes—not replay content.

## Requirements

- Functional: no transformed/reduced `tool_result`, assistant text, or historical reasoning in the model request.
- Non-functional: request prefix remains stable across adjacent passes except for the genuine current-turn suffix.

## Candidate Paths

1. **Local compiler cost dominates:** avoid repeated projection/encoding in one provider loop by caching an immutable compiled prefix keyed by `context_version`, mode, model prompt variant, and tool catalog version. Append only current-turn messages. Invalidate on any history/rules/tools change.
2. **Cache frontier breaks:** repair ordering/dynamic prompt placement so system prompt, rules, persisted history, and tool catalog stay byte-stable before latest-only reminders. Use the existing `StableMessageCount`, `CanonicalBodyHash`, and `FrontierHash` diagnostics to verify.
3. **Provider-pass count dominates:** identify redundant resume/retry/tool-loop transitions and eliminate only semantically duplicate provider calls, retaining required tool protocol steps.
4. **Provider reasoning dominates with cache stable:** do not change history. Expose provider/model configuration as the operational remedy; the code cannot locally speed model inference without a provider capability change.

## Related Code Files

- Modify only paths proven by phase 2, expected among:
  - `internal/backend/forwarder/compiler.go`
  - `internal/backend/forwarder/service.go`
  - `internal/backend/forwarder/projector.go`
  - `internal/backend/forwarder/artifacts.go`
  - `internal/backend/agent/model/openai.go`
- Add focused tests alongside the selected path.

## Implementation Steps

1. Select exactly one measured bottleneck and document the evidence in the implementation PR/commit.
2. Implement the smallest lossless optimization for that bottleneck.
3. Add equivalence tests asserting messages, tools, request mode, and reasoning/tool metadata are unchanged.
4. Compare adjacent request frontier/cache diagnostics before and after the change.
5. Do not proceed to another candidate without a new measurement pass.

## Success Criteria

- [ ] The selected implementation is justified by phase-2 data.
- [ ] Provider-visible replay equivalence tests pass.
- [ ] No additional provider pass is removed unless its duplicated semantics are demonstrated.
- [ ] Measured latency improves or the change is reverted.

## Risk Assessment

Caching compiled prefixes has invalidation risks. The cache must be derived-only, bounded, and fail open to normal compilation on any uncertainty.