# Phase 1 proposal — Task 1 approved

## Repository inspection

The repository at `D:\Github\quant-firm-simulation` was inspected on 2026-09-15. It contains an initialized Git repository on `main`, with no commits or working files before this document transfer. No ancestor AGENTS.md was found. The proposal and root AGENTS.md were copied from the previous planning workspace; no application code or module scaffolding has been created.

## Scope and architecture

Use one Go executable, one serial event loop, and local files. Packages are boundaries inside the process, not independently deployed services. Begin with deterministic CSV replay as paper market data. A network feed can be considered later.

```text
CSV market data -> Engine -> Strategy -> Order intent
                             |
                             v
                         Risk check -> rejection with reason
                             |
                      approval + reservation
                             v
                     Order management -> Paper broker
                                              |
                                             Fill
                                              |
                                    Position / cash / PnL
                                              |
                                  next risk/strategy snapshot

Engine commits state transitions to a local journal.
Structured logs and a final run summary expose the flow.
```

### Proposed first operating constraints

- One strategy, one configured instrument, one currency, whole units, long-only, no leverage.
- Market orders only; no cancel/replace API, partial fills, corporate actions, or external brokerage integrations in this phase.
- A toy strategy emits deterministic intents for plumbing tests. Its parameters and paper starting cash are explicit fixture inputs, not financial advice or optimized defaults.
- Data records include stable identity, UTC time, symbol, bid, and ask. Validate positive prices, bid <= ask, identity conflicts, and ordering. Freshness uses simulated time during replay.
- Fill an accepted order on the next valid quote, buys at ask and sells at bid, only if it remains within reserved cash/position capacity and configured limits. Otherwise reject the unfilled order with a reason and release its reservation. Use full fills with explicitly configured fees; this deliberately omits liquidity and queue modeling.
- A quote first resolves previously accepted orders, then marks the portfolio, then reaches the strategy. An order generated from that quote cannot fill on the same quote. EOF closes outstanding orders as unfilled and releases reservations through recorded terminal transitions.

## Components and responsibilities

1. **Domain:** typed quotes, intents, orders, fills, IDs, fixed-point values, and state-transition rules. No I/O or strategy rules.
2. **Market data:** parse and validate CSV records; provide stable source IDs and ordered events. No trading decisions.
3. **Engine:** own all mutable state; sequence events; supply immutable snapshots; orchestrate risk, OMS, broker, and accounting. Inject the clock and process one event to completion before the next.
4. **Strategy:** consume market events and portfolio snapshots; return zero or more intents. No broker, persistence, or ledger access. Start with a stateless fixture strategy so recovery requires no hidden strategy state.
5. **Risk:** reject invalid sizes, unsupported symbols, stale data, insufficient cash/holdings, and configured order/position limits. Include outstanding reservations and estimated fees. Approval and reservation happen together before submission.
6. **Order management (OMS):** map stable intent IDs to order IDs; enforce lifecycle; deduplicate retries; retain rejections and submission outcomes; release reservations once on terminal outcomes. Initial states: pending submission, accepted, filled, rejected, expired. A risk rejection records an intent outcome without submitting an order.
7. **Paper broker:** accept only approved orders, deduplicate submissions by order ID, and produce deterministic next-quote fills or terminal failures. Never access a real broker. Retries return the existing outcome; they do not create fresh orders.
8. **Portfolio:** apply unique fills once; update cash, holdings, average cost, realized PnL, and fees. Mark unrealized PnL at the current midpoint, recording mark time. Define PnL as realized plus unrealized minus fees, with cost-basis rounding documented.
9. **Journal and operations:** append ordered transition records, flush durable commits, recover state and deduplication indexes, and emit structured logs plus summary counters. No dashboard or monitoring service is needed initially.

## Reliability contract

Journal persistence, recovery, and crash/restart verification are introduced in Task 10, after the basic in-memory trading flow works. The durability and recovery requirements below describe the completed Phase 1 system; earlier tasks do not provide crash durability. Only Task 1 is currently approved for implementation.

Use a local append-only journal as the recovery source, separate from diagnostic logs. Each committed record contains the input identity, resulting domain events/state changes, reservations, generated IDs, and consumed CSV cursor. On restart, apply recorded transitions without invoking the strategy again; then continue at the next input record using the same configuration and input fingerprint.

Journal and sync a transition before making it visible to later processing. A failed commit halts the run. Crash before commit means retry the same input and identities; crash after commit means restore its recorded effects. Keep broker pending orders in recoverable state, so a retried submission or replayed fill cannot duplicate execution or accounting. This is practical because the paper broker is entirely local and has no external side effects.

Use sequence numbers and record integrity validation. For Phase 1, an incomplete or corrupt journal stops recovery with a clear diagnostic; automatic repair and snapshots are deferred. Require a single writer per run. Include a journal schema version and reject unsupported versions. A resumed run must match its input/configuration fingerprint; an explicitly new run gets a separate journal.

Risk approvals reserve cash or units before another intent is processed. Because next-quote prices may move, revalidate fill affordability and limits before committing a fill; reject rather than allow negative cash or short positions. Keep this rule visible as a simulation simplification.

Observability: structured logs include run ID, event sequence, source event ID, intent/order/fill IDs where applicable, and reason codes. Summaries include quotes processed, rejected intents/orders, duplicates ignored, accepted orders, fills, pending count, cash, position, realized/unrealized PnL, and fees. Graceful shutdown stops input and finishes the current commit; pending orders remain recoverable.

## Proposed directory structure

Create these only as implementation tasks need them. Tests live beside their Go packages.

```text
AGENTS.md
README.md
go.mod
cmd/paper/main.go             # configuration, wiring, lifecycle
internal/
  domain/                    # events, values, IDs, order states
  engine/                    # serial loop and integration tests
  marketdata/                # CSV replay and validation
  strategy/                  # strategy contract and toy fixture
  risk/                      # checks and reservation calculations
  oms/                       # order lifecycle and idempotency
  paper/                     # deterministic simulated broker
  portfolio/                 # cash, positions, accounting
  journal/                   # append, validation, recovery
testdata/                    # tiny deterministic CSV fixtures
configs/paper.example.json   # explicit simulation settings
docs/                       # accepted design and operating notes
outputs/phase-1-plan.md       # this review proposal
```

Use Go's standard configuration, CSV, and structured logging facilities where suitable. Avoid a generic event-bus framework, interfaces for every type, and a dependency injection framework. Keep runtime journals out of version control. The eventual README will document their location.

## First 10 tasks, in implementation order

1. **Bootstrap:** create the Go module and minimal CLI in the existing Git repository, validate paper-only configuration, and document build/test/run commands. Acceptance: tests pass, invalid configuration is rejected, and the executable prints a PAPER startup message. No trading components are implemented.
2. **Define domain contracts:** implement IDs, fixed-point arithmetic, event envelope, order lifecycle, and cost/fee rounding. Acceptance: boundary/overflow tests and invalid-transition tests pass.
3. **CSV market-data replay:** parse and validate CSV records with stable identities and deterministic ordering. Acceptance: invalid, duplicate, conflicting, and out-of-order records behave as specified, and fixtures replay deterministically.
4. **Toy strategy:** define the strategy contract and a stateless educational fixture that emits deterministic intents without execution access. Acceptance: known quotes and snapshots produce expected intents without future data.
5. **Risk:** validate limits, holdings, cash, fees, and freshness with pending reservations included. Acceptance: multiple outstanding intents cannot reuse reserved resources; rejection changes no holdings.
6. **Order management:** track intent-to-order identity, submission state, terminal outcomes, and reservation release in memory. Acceptance: duplicate intents/submissions and terminal events retain one order and release capacity once; no path bypasses risk.
7. **Paper broker:** implement next-quote full fills, affordability recheck, deterministic fill IDs, and EOF expiration. Acceptance: no same-quote fills, no duplicate fills, no negative cash/short positions, and explicit results for price gaps.
8. **Portfolio / PnL:** apply fills idempotently, calculate average cost and realized/unrealized PnL, and mark quotes. Acceptance: hand-calculated buy/sell/fee examples reconcile; duplicate fills leave balances unchanged.
9. **Integration and observability:** wire the serial engine and complete in-memory flow, structured logs, graceful shutdown, and run summary. Acceptance: one local command produces a traceable quote-to-fill-to-PnL run with manually verifiable expected balances.
10. **Journal, recovery, and crash/restart verification:** add single-writer ownership, durable commits, schema/integrity checks, input/configuration fingerprints, and state restoration. Test restart around acceptance/fill commits, duplicate delivery, storage failures, corrupt journals, and shutdown; document replay/resume/new-run commands. Acceptance: failures halt clearly, and uninterrupted and resumed runs produce identical orders, fills, balances, and domain outcomes.

## Phase 1 completion criteria

A reproducible local paper run demonstrates the whole flow; significant invariants have tests; restart preserves identities and balances; each rejection/fill is traceable; and the program contains no live-execution path. Profitability is not an acceptance criterion. Separate research/review/testing agents, live feeds, multiple assets, and performance optimization are future work.
