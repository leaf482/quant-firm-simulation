# Project-wide engineering rules

## Purpose and current scope
- Build a personal learning project about trading-system engineering, not profit optimization.
- Production code uses Go. Phase 1 is paper trading only in one process.
- The architecture and task order are recorded in `outputs/phase-1-plan.md`. Tasks 1 (bootstrap) and 2 (minimal domain contracts) are approved; subsequent implementation tasks require user approval.
- Do not implement live trading, broker credentials, or a configuration switch that enables live execution. Never enable live trading automatically.
- Do not make financial decisions for the user. Strategies and parameters are educational fixtures, not recommendations.

## Design and correctness
- Keep the system simple. Use a modular monolith; avoid services, distributed queues, speculative abstractions, and performance work without evidence.
- Modify only what is necessary for the current task. Explain dependencies before expanding scope.
- Separate strategy decisions from risk, order management, broker execution, and accounting. Strategies emit intents and never call brokers.
- All new orders must pass risk checks before submission. Reserve capacity for outstanding orders so repeated intents cannot overspend or exceed limits.
- Every order-related operation must account for duplicate events and retries, including intents, submissions, rejections, fills, and recovery.
- Use stable event, intent, order, and fill IDs. Retrying an operation must reuse its identity; conflicting payloads for the same ID must fail visibly.
- Apply each fill to cash, positions, and realized PnL at most once. Never promise exactly-once delivery.
- Define valid order states and transitions explicitly. Do not retry indefinitely or silently swallow failures.
- Use a single owner of mutable trading state initially. Keep event ordering deterministic and explicit.
- Use integer units for quantity and fixed-point money/prices with documented scales, rounding, and overflow checks; do not use floating point for ledger state.
- Inject time and any randomness. Record event time separately from processing time; tests must not depend on wall-clock sleeps.
- Reject invalid, stale, or unsupported data and fail closed on risk/configuration errors.
- State paper-fill assumptions explicitly. Never use future market data to make a strategy decision.
- Journal and recovery are deferred to Task 10, after the basic in-memory trading flow works. Once implemented, persist a committed transition before exposing its effects, halt on journal failure, and reject corrupt recovery input instead of silently resetting state. Earlier tasks must not claim crash durability.

## Tests, operations, and reviews
- Test significant behavior: risk limits, outstanding reservations, order transitions, duplicates, retries, accounting, deterministic replay, and recovery.
- Prefer focused unit tests plus a small end-to-end fixture; do not test private implementation details merely to increase coverage.
- Once a Go module exists, format Go changes and run `go test ./...` and `go vet ./...`; run `go test -race ./...` where supported for state/concurrency changes.
- Report which checks ran and any checks that could not run. Never claim unexecuted tests passed.
- Emit structured logs with run/event/order IDs and explicit rejection/failure reasons. Keep secrets out of logs and source control.
- Make shutdown and recovery behavior explicit. Do not discard accepted work silently.
- Keep dependencies minimal and justify additions. Document operating commands and simulation limitations.
- Reviewers prioritize safety boundaries, accounting invariants, retry behavior, and regressions before style.
- Future development, review, testing, and research agents follow these same rules. Do not introduce agent orchestration in Phase 1.
