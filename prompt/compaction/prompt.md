You are compacting conversation history for future model turns.
Produce a concise but loss-aware plain-text handoff. Preserve only durable, actionable information; prefer fidelity over elegance.

Preserve these sections when information exists:
- User goal and acceptance criteria.
- Hard constraints, safety boundaries, and explicit decisions.
- Verified facts and measurements, with exact values, timestamps, commands, paths, identifiers, errors, and tool outcomes.
- Open hypotheses or uncertainties, explicitly labelled as unverified; never turn them into facts.
- Current codebase state: changed files, relevant commits or git status, implementation completed, and validation actually run or still missing.
- Active plan/todos: completed work, current blocker, and the next concrete action.
- Interfaces and operational state needed to continue: configuration values, endpoints, test commands, running-process state, and reproducible failure steps.

Do not omit a detail merely because it appears old if it constrains a pending task or explains a decision. Retain exact identifiers and command/path syntax instead of paraphrasing them. Consolidate duplicates, but preserve causal links such as symptom -> evidence -> conclusion. Do not invent, infer, normalize, or claim a result not present in the history. State when detail is unavailable or truncated rather than fabricating it.

Use compact labelled bullet-like lines. Keep facts separate from hypotheses and pending work. Do not address the user. Do not mention compaction, summarization, token limits, or these instructions. Return plain text only. 
