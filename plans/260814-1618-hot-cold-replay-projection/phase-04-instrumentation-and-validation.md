---
phase: 4
title: "Benchmark, Rollout, and Decision Gate"
status: pending
priority: P1
effort: "0.5-1d"
dependencies: [2, 3]
---

# Phase 4: Benchmark, Rollout, and Decision Gate

## Overview

Validate the chosen lossless optimization with controlled A/B evidence, retain a quick rollback, and make an explicit decision when the actual bottleneck is provider-side rather than local.

## Requirements

- Functional: compare identical replay fixtures and comparable real conversations with the same model/channel.
- Non-functional: no performance claim without fresh timing evidence.

## Related Code Files

- Modify: `internal/historymetrics/report.go` and `scripts/historymetrics/main.go` if phase 2 adds reportable metrics.
- Modify/Add: targeted forwarder performance/equivalence tests.
- No history-store schema migration unless phase 3 measurement proves it necessary.

## Implementation Steps

1. Run feature-off/on equivalence tests for provider-visible messages and checkpoint/history persistence.
2. Run short and long conversation A/B measurements using the same model, endpoint, reasoning setting, and stable prompt input.
3. Compare compile duration, TTFT, total provider duration, cache-read ratio, frontier continuity, and provider-pass count.
4. Accept only improvements that preserve replay equivalence and do not regress adjacent cache behavior.
5. If provider TTFT/reasoning remains dominant under stable cache, report it as an upstream/model limitation and keep the local behavior unchanged.
6. Run targeted tests followed by `go test ./...`; document rollout/rollback for any selected derived cache.

## Success Criteria

- [ ] The benchmark includes a long coding conversation near the user's operational context size.
- [ ] Every accepted improvement is lossless to the provider-visible replay contract.
- [ ] Cache and latency conclusions are based on actual provider timing/usage evidence.
- [ ] Full test suite passes.

## Risk Assessment

A faster local compile cannot compensate for slow upstream inference. This gate prevents shipping a risky context transformation when data shows the provider is the bottleneck.