---
phase: 2
title: "Trace Per-Pass Latency and Cache Behavior"
status: completed
priority: P1
effort: "1d"
dependencies: [1]
---

# Phase 2: Trace Per-Pass Latency and Cache Behavior

## Overview

Identify whether latency growth is local compilation/serialization, cache-frontier instability, provider TTFT, provider reasoning, or excessive provider passes. Do not infer a fix from prompt size alone.

## Requirements

- Functional: measure each provider pass from request prepared to first provider event and terminal event.
- Functional: correlate timing with model call, prompt tokens, cache-read tokens, frontier metadata, and pass count.
- Non-functional: preserve request construction and messages exactly.

## Related Code Files

- Modify: `internal/backend/forwarder/service.go`
- Modify: `internal/backend/forwarder/artifacts.go`
- Modify: `internal/backend/forwarder/token_usage.go`
- Modify: `internal/historymetrics/report.go`
- Modify: `scripts/historymetrics/main.go`

## Implementation Steps

1. Attach bounded timing state to an active provider pass: compilation complete, request prepared, first provider event, and terminal event.
2. Persist only numeric summaries keyed by request/model-call/provider pass; do not write full provider bodies outside existing opt-in debug logs.
3. Report replay message count, estimated prompt tokens, cache-read tokens when supplied, expected/actual frontier indicators, TTFT, total pass duration, and pass count.
4. Run controlled short-vs-long baseline conversations with the same model/configuration and `log: true` only during diagnosis.
5. Classify each bottleneck with evidence before choosing an optimization path.

## Success Criteria

- [ ] A single conversation can show timing and token evidence for every provider pass.
- [ ] Missing provider usage fields are reported as unavailable, never as a cache miss.
- [ ] Evidence distinguishes local compile cost from upstream/provider wait.

## Risk Assessment

TTFT includes upstream scheduling and reasoning; it supports comparison, not an assertion that the local backend caused all delay.