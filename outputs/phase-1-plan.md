# Phase 1 proposal — Tasks 1–7 approved

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
- Task 7 fills a valid SUBMITTED order immediately using the caller's current quote: buys at ask and sells at bid, in full, with no fees, slippage, latency, or account checks inside the broker. This supersedes the original next-quote execution proposal.
- The broker does not schedule quotes, queue orders, or expire orders at EOF. Integration and account/reservation handling are deferred; the eventual caller chooses the quote and updates OMS/account state after receiving a fill.

## Components and responsibilities

1. **Domain:** typed quotes, intents, orders, fills, IDs, fixed-point values, and state-transition rules. No I/O or strategy rules.
2. **Market data:** parse and validate CSV records; provide stable source IDs and ordered events. No trading decisions.
3. **Engine:** own all mutable state; sequence events; supply immutable snapshots; orchestrate risk, OMS, broker, and accounting. Inject the clock and process one event to completion before the next.
4. **Strategy:** consume validated quotes and return at most one intent. PriceMovement stores the selected symbol, previous rounded midpoint, and intent sequence. No broker, persistence, portfolio, or ledger access. Task 10 must recover this strategy state.
5. **Risk:** reject invalid sizes, unsupported symbols, stale data, insufficient cash/holdings, and configured order/position limits. Include outstanding reservations and estimated fees. Approval and reservation happen together before submission.
6. **Order management (OMS):** map stable intent IDs to deterministic order IDs and deduplicate matching retries; reject conflicting payloads. States are NEW, SUBMITTED, CANCELLED, REJECTED, and FILLED. Task 7 adds SUBMITTED -> FILLED. Creation assumes prior risk approval; OMS does not call the broker or handle reservations.
7. **Paper broker:** accept valid SUBMITTED orders and matching quotes, return deterministic full fills at current ask/bid, and deduplicate by OrderID. Conflicting order payloads fail. Return a fill without changing OMS or account state; never access a real broker.
8. **Portfolio:** apply unique fills once; update cash, holdings, average cost, realized PnL, and fees. Mark unrealized PnL at the current midpoint, recording mark time. Define PnL as realized plus unrealized minus fees, with cost-basis rounding documented.
9. **Journal and operations:** append ordered transition records, flush durable commits, recover state and deduplication indexes, and emit structured logs plus summary counters. No dashboard or monitoring service is needed initially.

## Reliability contract

Journal persistence, recovery, and crash/restart verification are introduced in Task 10, after the basic in-memory trading flow works. The durability and recovery requirements below describe the completed Phase 1 system; earlier tasks do not provide crash durability. Tasks 1–7 are currently approved for implementation.

Use a local append-only journal as the recovery source, separate from diagnostic logs. Each committed record contains the input identity, resulting domain events/state changes, reservations, generated IDs, and consumed CSV cursor. On restart, apply recorded transitions without invoking the strategy again; then continue at the next input record using the same configuration and input fingerprint.

Journal and sync a transition before making it visible to later processing. A failed commit halts the run. Crash before commit means retry the same input and identities; crash after commit means restore its recorded effects. Keep broker pending orders in recoverable state, so a retried submission or replayed fill cannot duplicate execution or accounting. This is practical because the paper broker is entirely local and has no external side effects.

Use sequence numbers and record integrity validation. For Phase 1, an incomplete or corrupt journal stops recovery with a clear diagnostic; automatic repair and snapshots are deferred. Require a single writer per run. Include a journal schema version and reject unsupported versions. A resumed run must match its input/configuration fingerprint; an explicitly new run gets a separate journal.

Reservation and account consistency across approval and execution remain future integration requirements. Task 7 does not recheck affordability or risk within the broker; its caller must eventually prevent overspending and short positions using the quote and account state selected for execution.

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
2. **Define minimal domain contracts:** implement Symbol, Price, Quantity, Side, Quote, OrderIntent, and IntentID only. Use int64 prices in units of $0.0001, strict decimal parsing and formatting, whole-share quantities, and validation. Acceptance: parsing/formatting boundary tests and quote, quantity, and intent validation tests pass. Arithmetic, event envelopes, order lifecycle, and cost/fee accounting are deferred to the components that need them.
3. **CSV market-data replay:** read the exact timestamp,symbol,bid,ask schema sequentially into validated domain quotes. Acceptance: invalid rows report their CSV record number, timestamps cannot move backwards, equal timestamps retain file order, and completion returns EOF without wall-clock timing. Source IDs, deduplication, and identity conflict detection are deferred; the current schema has no identity field and repeated valid rows are replayed in file order.
4. **Toy strategy:** implement a stateful PriceMovement type that compares consecutive integer midpoints and emits deterministic one-share BUY/SELL intents on rises/falls. The first quote and equal midpoints produce no signal. Acceptance: known sequences, rounding/overflow boundaries, validation, and deterministic run-local IDs pass tests. No replay or CLI integration.
5. **Risk:** implement a side-effect-free checker against a supplied quote, available cash, held quantity, and positive limits. BUY uses ask and checks cash, maximum order notional, and maximum position; SELL uses bid and checks holdings. Validate inputs and reject arithmetic overflow. Acceptance: boundary, rejection, and no-mutation tests pass. Reservations, fees, freshness checks, and integration are deferred.
6. **Order management:** create validated NEW orders from caller-approved intents, deduplicate by IntentID, and maintain explicit lifecycle transitions in memory. Acceptance: matching retries return the current order; conflicts and invalid transitions leave state unchanged; IDs are deterministic. No broker submission, fills, risk rechecks, or reservations.
7. **Paper broker:** execute valid SUBMITTED orders fully at the supplied quote's ask/bid, use its timestamp, validate fills, and generate deterministic IDs. Deduplicate executions and reject conflicting OrderID reuse. Add SUBMITTED -> FILLED to OMS without coupling components. Acceptance: deterministic prices/timestamps/IDs, invalid-input rejection, no duplicate fills, and lifecycle tests pass. Account mutation, risk checks, fees, and integration are deferred.
8. **Portfolio / PnL:** apply fills idempotently, calculate average cost and realized/unrealized PnL, and mark quotes. Acceptance: hand-calculated buy/sell/fee examples reconcile; duplicate fills leave balances unchanged.
9. **Integration and observability:** wire the serial engine and complete in-memory flow, structured logs, graceful shutdown, and run summary. Acceptance: one local command produces a traceable quote-to-fill-to-PnL run with manually verifiable expected balances.
10. **Journal, recovery, and crash/restart verification:** add single-writer ownership, durable commits, schema/integrity checks, input/configuration fingerprints, and state restoration. Test restart around acceptance/fill commits, duplicate delivery, storage failures, corrupt journals, and shutdown; document replay/resume/new-run commands. Acceptance: failures halt clearly, and uninterrupted and resumed runs produce identical orders, fills, balances, and domain outcomes.

## Phase 1 completion criteria

A reproducible local paper run demonstrates the whole flow; significant invariants have tests; restart preserves identities and balances; each rejection/fill is traceable; and the program contains no live-execution path. Profitability is not an acceptance criterion. Separate research/review/testing agents, live feeds, multiple assets, and performance optimization are future work.
