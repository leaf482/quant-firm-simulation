# Phase 1 proposal — Tasks 1–10 approved

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
                      risk approval
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
- The broker does not schedule quotes, queue orders, or expire orders at EOF. Task 9 executes with the same current quote and applies the fill before the next quote; the engine updates OMS/account state. No outstanding orders require reservations.

## Components and responsibilities

1. **Domain:** typed quotes, intents, orders, fills, IDs, fixed-point values, and state-transition rules. No I/O or strategy rules.
2. **Market data:** parse and validate CSV records; provide stable source IDs and ordered events. No trading decisions.
3. **Engine:** own all mutable state; sequence events; supply immutable snapshots; orchestrate risk, OMS, broker, and accounting. Inject the clock and process one event to completion before the next.
4. **Strategy:** consume validated quotes and return at most one intent. PriceMovement stores the selected symbol, previous rounded midpoint, and intent sequence. No broker, persistence, portfolio, or ledger access. Strategy-state recovery is explicitly deferred beyond Task 10.
5. **Risk:** reject invalid sizes, unsupported symbols, stale data, insufficient cash/holdings, and configured order/position limits. Include outstanding reservations and estimated fees. Approval and reservation happen together before submission.
6. **Order management (OMS):** map stable intent IDs to deterministic order IDs and deduplicate matching retries; reject conflicting payloads. States are NEW, SUBMITTED, CANCELLED, REJECTED, and FILLED. Task 7 adds SUBMITTED -> FILLED. Creation assumes prior risk approval; OMS does not call the broker or handle reservations.
7. **Paper broker:** accept valid SUBMITTED orders and matching quotes, return deterministic full fills at current ask/bid, and deduplicate by OrderID. Conflicting order payloads fail. Return a fill without changing OMS or account state; never access a real broker.
8. **Portfolio:** apply validated fills once by FillID, rejecting conflicting reuse, insufficient cash/holdings, wrong symbols, and overflow without mutation. Track initial/current Money cash and whole-share position. Read-only snapshots value holdings at bid; total PnL is cash plus market value minus initial cash. Cost basis, realized/unrealized split, and fees are deferred.
9. **Journal and operations:** append ordered transition records, flush durable commits, recover state and deduplication indexes, and emit structured logs plus summary counters. No dashboard or monitoring service is needed initially.

## Reliability contract

Task 10 adds optional append-only JSON Lines with schema version, consecutive sequence, and account metadata on each record. Only order_created, order_state_changed, and fill_applied events are persisted. New journals use exclusive file creation; existing files are inspected through a separate read-only recovery mode.

Each operation is validated in private tentative state, then its complete record is written and File.Sync succeeds before dependent processing, committed counters, or event logs. Commit order: NEW order, SUBMITTED transition, applied fill, FILLED transition. On any journal failure the run halts and discards private components. A failed sync is uncertain: any complete surviving record is subject to normal recovery validation.

Recovery starts fresh and rebuilds OMS, portfolio, broker fill deduplication, and deterministic ID sequences from the journal alone. It rejects malformed or incomplete lines, sequence errors, invalid values, conflicting identities, and impossible histories. Complete prefixes preserve their exact boundary; recovery never invents a missing transition. Empty journals have no metadata and cannot reconstruct an account.

Strategy state, CSV cursor/fingerprint, automatic resume, snapshots, compaction, and tail repair are explicitly deferred. A supplied quote is required to value recovered holdings; CLI inspection shows orders, cash, and position only. This targets process termination with synced file writes, not a full transactional database or power-loss namespace durability.

The synchronous engine settles one order at the same quote before considering another. No outstanding-order reservations or concurrent writers are introduced. A subprocess test exits without closing the journal, and fresh recovery must match balances/order states and continue order/fill IDs without collision.

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
8. **Portfolio / PnL:** introduce distinct fixed-point Money and safe Price times Quantity arithmetic; migrate risk monetary values. Implement atomic, idempotent fill application and bid-based snapshots of cash, position, market value, equity, and total PnL. Acceptance: hand-calculated buys/sells and positive/negative PnL reconcile; duplicates and failed operations preserve state; arithmetic boundaries are tested. No cost basis, realized/unrealized split, fees, or integration.
9. **Integration and observability:** run replay, strategy, risk, OMS, paper execution, and portfolio synchronously at the same quote; continue only on typed trading rejections. Add small CLI flags, deterministic standard-library logs, and exact end-to-end summary tests. No concurrency, persistence, recovery, or background shutdown machinery. Acceptance: one command completes the fixture with exact balances, and system failures halt without a success summary.
10. **Journal, recovery, and crash/restart verification:** add exclusively created JSONL journals, synced trading records, strict sequence/lifecycle checks, and fresh OMS/portfolio/broker recovery. Test complete prefixes, process termination, corruption, write/sync failure, and continued IDs. CLI supports separate new-run and recovery-inspection modes; strategy recovery and simulation resume are deferred.

## Phase 1 completion criteria

A reproducible local paper run demonstrates the whole flow; significant invariants have tests; restart preserves identities and balances; each rejection/fill is traceable; and the program contains no live-execution path. Profitability is not an acceptance criterion. Separate research/review/testing agents, live feeds, multiple assets, and performance optimization are future work.
