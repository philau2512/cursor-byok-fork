---
title: "Lossless Long-Context Performance Investigation"
description: "Measure and optimize long-running agent latency without removing model-visible historical detail or lowering the existing ~300k compaction policy."
status: pending
priority: P1
effort: "2-4d"
tags: [forwarder, prompt-replay, performance, cache, lossless]
created: 2026-08-14
---

# Lossless Long-Context Performance Investigation

## Overview

The original hot/cold digest proposal is **not lossless for the model**: a digest replaces raw content in `ProjectPromptReplay()`, so the provider no longer receives the omitted detail. Although `context.json` retains the original payload, the agent has no generic tool to recall arbitrary archived history verbatim. That could reduce reliability for a heavy coding task.

This revised plan preserves all provider-visible history—including completed tool results and historical reasoning—until the existing ~300k compaction policy acts. It investigates and optimizes latency at the lossless boundaries: per-pass behavior, request construction, stable-prefix/cache behavior, and redundant work. Any replay reduction remains out of scope unless a future explicit lossiness decision is made.

## Validated Constraints

- **Retention policy:** lossless. Digest only output that is provably obsolete/superseded was initially selected, but that is still not strict model-visible losslessness; therefore v1 does not digest any raw replay.
- **Working-memory policy:** raw transcript only. No semantic state ledger or LLM-generated summary beyond existing compaction.
- **Existing compaction:** retain the ~300k operating policy; do not lower it as a latency workaround.
- **Reasoning:** preserve replayed reasoning/signature content unchanged.

## Architecture Focus

```text
context.json (canonical and model-visible replay source)
        |
        v
HistoryProjector.ProjectPromptReplay()  -- preserve output byte-for-byte
        |
        v
DefaultPromptCompiler / ProviderRequest -- measure compile and request stages
        |
        v
OpenAI-compatible provider -- measure TTFT, pass count, cache usage, total duration
```

## Phases

| # | Phase | Status | Dependency |
|---|-------|--------|------------|
| 1 | [Establish lossless baseline](./phase-01-start.md) | Completed | — |
| 2 | [Trace per-pass latency and cache behavior](./phase-02-projection-policy.md) | Completed | 1 |
| 3 | [Lossless execution-path optimizations](./phase-03-semantic-tool-digests.md) | Pending | 2 |
| 4 | [Benchmark, rollout, and decision gate](./phase-04-instrumentation-and-validation.md) | Pending | 2, 3 |

## Success Criteria

- [x] The same persisted conversation produces the same provider-visible replay with optimization enabled and disabled.
- [x] `context.json`, checkpoint projection, tool ordering, and historical reasoning replay remain unchanged.
- [x] Per-pass TTFT, provider duration, prompt size, cache-read usage, and provider-pass count can be compared for the same conversation/model.
- [ ] Any shipped optimization has A/B evidence of latency improvement without a prefix-cache regression.
- [x] Targeted forwarder tests and `go test ./...` pass.

## Explicit Non-goals

- Replacing raw tool results with semantic digests.
- Dropping or summarizing historical reasoning/reasoning signatures.
- Reducing the 300k compaction threshold.
- Provider-native continuation (`previous_response_id`) until the local router proves compatible.

## Validation Log

### Validation Session 1 — 2026-08-14

- **Decision:** prioritize strict model-visible context retention; do not make the agent re-read code or reconstruct historical tool output as a condition of correct continuation.
- **Decision:** do not add a separate working-memory ledger or model-generated semantic summary.
- **Plan change:** replaced hot/cold projection/digest delivery with a lossless latency diagnosis and optimization plan.

### Verification Results

- Claims checked: 8
- Verified: 8 | Failed: 0 | Unverified: 0
- Tier: Standard
- Evidence:
  - `ProjectPromptReplay()` renders `tool_result` payloads into provider messages in `internal/backend/forwarder/projector.go`.
  - Existing `compactedPromptProjectionEntries()` excludes old replay entries after a summary marker, proving a projection change changes model-visible context while retaining canonical history.
  - Existing truncation in `internal/backend/forwarder/tool_result_replay_truncation.go` is replay-only, not a history-store mutation.
  - `DefaultPromptCompiler.Compile()` obtains provider replay from `ProjectPromptReplay()`.

### Implementation Session 2 — 2026-08-14

- Added a lossless compiler fixture covering historical reasoning signatures, tool calls/results, and an extended replay sequence. It asserts `ProjectPromptReplay()` and `DefaultPromptCompiler.Compile()` preserve the same provider-visible messages and ordering.
- Added bounded per-provider-pass diagnostics: compile duration, estimated prompt tokens, replay message count, TTFT, terminal duration, cache-read tokens/availability, and provider-pass number. These are numeric summaries only; provider request bodies remain confined to existing opt-in debug logging.
- Extended `historymetrics` to list retained provider-pass metrics so short-vs-long runs can be compared without interpreting absent cache usage as a cache miss.
- Baseline benchmark: 100 iterations of the representative compiler fixture measured `1,003,682 ns/op`, `487,190 B/op`, and `1,316 allocs/op` on the local Windows host. This is a local compilation baseline, not provider latency evidence.
- Validation: targeted forwarder tests and `go test ./...` pass.


- Re-read `plan.md` and all phase files after the retention decision.
- Removed references to semantic digests, cold replay classifications, and token reduction via tool-result omission.
- Result: no unresolved contradiction.

<!-- slug: hot-cold-replay-projection -->
