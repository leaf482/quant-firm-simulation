This proposes **journal version 3** as the concrete Phase 2 contract. It preserves derived reservation state, atomic admission and settlement boundaries, and separate OMS `FILLED` finalization.

No files were modified, implementation written, or commits created. The working tree remains clean at `885edfd23f38bd34a980439a401727692f7454b7`. Downstream implementation remains blocked pending design approval.

**1. Exact v3 event types**

Version 3 permits exactly:

1. `run_started`
2. `order_admitted`
3. `order_submitted`
4. `fill_applied`
5. `order_filled`
6. `order_terminal`
7. `intent_denied`

`order_terminal` carries either `REJECTED` or `CANCELLED`. There are no standalone reservation acquisition/release events and no generic unrestricted state-change event.

`run_started` is required as journal sequence 1. Its empty payload establishes account metadata even for runs that produce no orders. A zero-byte journal remains invalid recovery input.

**2. Exact fields for each event**

The field orders listed below also define canonical checksum serialization order. Unless explicitly described as a tagged variant, every listed field is required and no additional field is permitted.

**Envelope and record**

- Envelope: `record`, `crc32c`.
- Record: `version`, `sequence`, `run`, `event`.
- Event: `type`, `payload`.

Types:

- `version`: integer, exactly `3`.
- `sequence`: positive `uint64`; first record is 1.
- `crc32c`: unsigned 32-bit integer.
- Prices, Money, quantities: signed 64-bit integer JSON values, subject to the semantic constraints below.
- Quote sequences and admission ordinals: positive `uint64`.
- IDs and enum values: JSON strings.
- Timestamps: nonzero UTC RFC3339Nano strings in canonical form, with `Z` and no unnecessary fractional trailing zeros.

No floating-point monetary values, fractional shares, numeric strings, or exponent-form integer values are accepted.

**Run metadata**

`run` contains, in this order:

- `run_id`: nonblank, caller-supplied stable run identity.
- `symbol`: nonblank configured symbol.
- `initial_cash`: nonnegative Money.
- `mode`: exactly `"PAPER"`.
- `money_scale`: exactly `10000`, applying to both Money and Price.
- `quantity_unit`: exactly `"WHOLE_SHARE"`.
- `position_policy`: exactly `"LONG_ONLY"`.
- `fill_policy`: exactly `"FULL_ONLY"`.
- `fee_policy`: exactly `"NONE"`.
- `quote_delay`: exactly `2`.
- `buy_reservation_policy`: exactly `"SUBMISSION_ASK_CAP"`.
- `risk_limits`: object containing `max_order_notional`, then `max_position`; both strictly positive.

Every value is immutable and repeated on every record. Recovery rejects any difference. Initial position is zero by definition of v3.

The run identity is injected, not generated from wall-clock time during trading. All trading IDs are scoped by run identity.

**Shared nested objects**

`intent`:

- `intent_id`
- `symbol`
- `side`: `"BUY"` or `"SELL"`
- `quantity`: positive whole shares
- `timestamp`

This preserves the complete current `OrderIntent` payload.

A quote object contains:

- `sequence`
- `timestamp`
- `bid`
- `ask`

The symbol is inherited from immutable run metadata. Prices must satisfy `0 < bid ≤ ask`. Retaining bid and ask permits recovery to validate risk decisions and execution prices without consulting external market data.

`reason` contains exactly:

- `code`

There is no persisted free-form message. Display text may be derived from the code and recorded context.

**`run_started`**

Payload: `{}`.

It occurs exactly once, at sequence 1.

**`order_admitted`**

Payload fields:

- `admission_id`
- `admission_ordinal`
- `order_id`
- `intent`
- `origin_quote`
- `status`: exactly `"NEW"`
- `reservation`

For distinct admissions, ordinal starts at 1 and increases consecutively. IDs are:

- `order_id = "order-" + decimal ordinal`
- `admission_id = "admission-" + decimal ordinal`

Decimals have no leading zeros. OMS owns this allocation; journal recovery reconstructs its committed high-water mark.

BUY reservation fields:

- `type`: `"BUY_CASH"`
- `reference_ask`
- `reserved_money`

Validation:

- `reference_ask == origin_quote.ask`
- `reserved_money == reference_ask × intent.quantity`, using checked arithmetic.
- `reserved_money > 0`.

SELL reservation fields:

- `type`: `"SELL_QUANTITY"`
- `reserved_quantity`

Validation:

- `reserved_quantity == intent.quantity`.

BUY fields must be absent from SELL reservations, and SELL fields must be absent from BUY reservations. Side and quantity are stored once in the intent and govern the order and reservation.

The intent symbol must equal the run symbol, and intent timestamp must equal origin-quote timestamp.

**`order_submitted`**

Payload fields:

- `order_id`
- `admission_id`
- `quote_sequence`
- `timestamp`

The sequence and timestamp must match the admitted order’s origin quote. Phase 2 submits during the same quote-processing cycle as admission.

Eligibility is **derived**, not stored:

`eligibleQuoteSequence = quote_sequence + 2`

Addition must be overflow checked before admission is committed. Retries cannot alter submission sequence or timestamp.

The event type establishes `SUBMITTED`; no redundant status field is stored.

**`fill_applied`**

Payload fields:

- `fill_id`
- `order_id`
- `admission_id`
- `symbol`
- `side`
- `quantity`
- `price`
- `execution_quote`

`FillID` is also **SettlementID**. No second settlement-ID field is stored.

Distinct fills use consecutive `fill-1`, `fill-2`, … identities in committed settlement order. Retries do not advance this allocator.

Validation:

- Order/admission references identify the same admitted order.
- Symbol, side, and quantity exactly match that order.
- Quantity is the full order quantity.
- BUY price equals execution quote ask.
- SELL price equals execution quote bid.
- Execution quote sequence equals original submission sequence plus 2.
- Fill timestamp is execution quote timestamp.
- Cost/proceeds are derived by checked `price × quantity`.

One OrderID can have exactly one committed settlement identity.

**`order_filled`**

Payload fields:

- `order_id`
- `admission_id`
- `fill_id`

This establishes OMS `FILLED`. The referenced settlement must already exist and match the order. It performs no accounting or reservation change.

**`order_terminal`**

Payload fields:

- `outcome_id`
- `order_id`
- `admission_id`
- `status`: `"REJECTED"` or `"CANCELLED"`
- `reason`
- `context`

Outcome identity is deterministically:

`"outcome/" + order_id`

There is one terminal-outcome slot per order; conflicting status, reason, or context is not another valid outcome.

Context is a tagged union.

QUOTE context:

- `type`: `"QUOTE"`
- `quote`: the shared quote object.

CONTROL context:

- `type`: `"CONTROL"`
- `command_id`: nonblank stable caller-supplied command identity.
- `after_quote_sequence`: positive sequence of the completed quote cycle after which the command was accepted.

Allowed combinations are exhaustive:

| Source state | Target | Reason code | Context |
|---|---|---|---|
| `NEW` | `REJECTED` | `SUBMISSION_DECLINED` | QUOTE, exactly the admission’s origin quote |
| Unsettled `SUBMITTED` | `REJECTED` | `BUY_RESERVATION_EXCEEDED` | QUOTE, exactly the eligible execution quote |
| Unsettled `SUBMITTED` | `REJECTED` | `REJECT_REQUESTED` | CONTROL |
| Unsettled `SUBMITTED` | `CANCELLED` | `CANCEL_REQUESTED` | CONTROL |

`SUBMISSION_DECLINED` represents an explicit pre-submission trading decision, not a journal, arithmetic, or invariant failure. The contract permits it without requiring a new production rejection mechanism.

CONTROL outcomes are accepted only between completed quote cycles, before execution eligibility. Thus:

`submissionSequence ≤ after_quote_sequence < eligibleSequence`

No cancellation interface is introduced by this design.

**`intent_denied`**

Payload fields:

- `denial_id`
- `intent`
- `origin_quote`
- `reason`

Denial identity is:

`"denial/" + intent.intent_id`

Allowed reason codes:

- `INSUFFICIENT_CASH`
- `MAX_ORDER_NOTIONAL`
- `MAX_POSITION`
- `INSUFFICIENT_HOLDINGS`

The original quote and complete intent permit replaying the risk check against preceding recovered state. Intent timestamp must match quote timestamp.

**Decision: persist risk denials.** Omitting them would lose rejected-intent retry outcomes after restart. Persisting them makes inspection and retry identity deterministic without creating orders or reservations.

Invalid input, overflow, and internal errors are fatal; they are not persisted as ordinary risk denials.

**3. Validation rules**

**Structure and integrity**

Preserve Phase 1’s strict JSONL integrity model:

- Exactly one complete JSON object per newline-terminated record.
- Missing final newline is a torn record, even if the JSON object parses.
- Null is forbidden at every level.
- Duplicate decoded member names are forbidden, including escaped duplicate spellings.
- Unknown fields and case variants are forbidden.
- Tagged variants require exactly their own fields.
- Trailing JSON, blank record lines, and malformed objects fail.
- All records must use the same schema version.

Checksum is CRC32C/Castagnoli over canonical `json.Marshal(record)` bytes, excluding envelope checksum and newline. Canonical objects use the field order specified above, compact encoding, and Go `encoding/json` string escaping. Payload variants use their declared field order; no arbitrary map determines checksum order.

Input JSON member ordering and insignificant whitespace do not affect the checksum. Decode strictly, reconstruct the canonical typed record, and recompute.

CRC detects accidental corruption; it is not authentication.

Journal sequences must be exactly `1, 2, …`, without gaps, repetition, overflow, or reset. A semantic retry recorded again uses a **new journal sequence**.

**Identity and retries**

Validate envelope integrity and immutable metadata before semantic deduplication.

For each logical event:

- An exact duplicate payload returns the historical result without reapplying effects.
- Any changed immutable field under an existing identity fails.
- Duplicate admission cannot allocate another ordinal or reactivate resources.
- Duplicate submission cannot move the original submission sequence.
- Duplicate Fill cannot mutate accounting or release again.
- Duplicate terminal outcome cannot release again.
- Duplicate `order_filled` cannot alter accounting.

A matching historical retry may appear after the order becomes terminal. It does not roll lifecycle state backwards.

Recovery retains enough original event data to compare the complete payload, not just its ID.

Additionally:

- An IntentID maps to exactly one admission or one denial.
- An admitted IntentID cannot later become denied, or vice versa.
- OrderID and AdmissionID cannot refer to different admissions.
- Numeric admission ordinals cannot collide or skip for distinct admissions.
- FillID cannot be reused for another order.
- A second distinct FillID for one full-fill order fails, even with identical economic terms.
- A CONTROL command identity cannot be reused for a different terminal operation.
- `run_started` is not repeatable within a journal.

For API retries, resolve identity before consulting a newer quote. The writer returns the retained result; it does not manufacture a changed duplicate record.

**Risk and reservation validation**

On first admission, re-evaluate approval using the recorded origin quote, immutable limits, and preceding recovered portfolio/reservations.

BUY requires:

- `ask × quantity ≤ actualCash − reservedCash`
- Notional no greater than `max_order_notional`.
- `actualPosition + outstandingBuy + newQuantity ≤ max_position`.

SELL requires:

- `quantity ≤ actualPosition − reservedSell`.

Outstanding SELLs do not reduce projected BUY position or supply cash. Outstanding BUYs are not sellable inventory.

Risk-denial recovery must reproduce the recorded rejection code. BUY denial precedence remains cash, order notional, then position. SELL uses holdings. Validate arithmetic first; arithmetic failure is fatal.

At every recovered boundary:

- Cash and actual position are nonnegative.
- Active reserved cash does not exceed actual cash.
- Active reserved sells do not exceed actual position.
- All aggregates and projected-position arithmetic fit their integer types.
- Each reservation’s immutable arithmetic is valid.

Settlement validates the prospective portfolio **and released reservation state together**. Do not reject a valid settlement by exposing the temporary combination of changed accounting and an unreleased reservation.

**Lifecycle and processing order**

New events, excluding exact historical duplicates, obey:

- Admission precedes submission.
- A pending `NEW` admission must be submitted or rejected before another distinct trading operation.
- Settlement requires unsettled `SUBMITTED`.
- Settlement after rejection/cancellation fails.
- Rejection/cancellation after settlement fails, even if OMS remains `SUBMITTED`.
- `FILLED` without settlement fails.
- After settlement, the next distinct trading operation must be its `order_filled`. Exact historical duplicates may intervene.
- `NEW → CANCELLED` fails.

A valid prefix may end at pending `NEW` or pending `FILLED` finalization.

For execution ordering, sort active eligible orders by:

1. Original submission quote sequence.
2. Numeric admission ordinal.

Resolve each completely before the next. Execute eligible old orders before a new admission or risk denial on the current quote.

New recorded quote contexts advance monotonically by sequence. The same sequence must have identical quote data; higher sequences cannot have earlier timestamps. Equal timestamps are allowed.

Before accepting a new context at K:

- No outstanding order may have eligibility less than K.
- A strategy admission/denial at K requires all orders eligible at K already resolved.
- An execution outcome at K must concern the first unresolved order in the eligible ordering.
- A CONTROL context after K requires no unresolved order eligible at or before K.

A larger sequence may skip quotes that produced no journal event. The trading journal does not prove the content of unrecorded CSV rows or serve as a replay cursor. This is an explicit inspection-only boundary, not permission to skip execution in the live event loop.

**Delayed execution and price policy**

The engine increments quote sequence once per accepted CSV quote, including equal timestamps. Invalid input halts before trading actions. Headers, blank lines, EOF, and retries do not increment it.

For an order submitted at N, the first execution attempt is exactly N+2. An unresolved active order cannot defer its attempt to N+3.

BUY calculates:

`actualCost = executionAsk × quantity`

Then compares Money with Money:

- `actualCost ≤ reservedMoney`: full settlement and complete reservation release.
- `actualCost > reservedMoney`: `BUY_RESERVATION_EXCEEDED`, unchanged portfolio, complete release.

No top-up, partial fill, or retry at a cheaper later quote. Arithmetic failure halts.

**4. Commit meaning of each event**

All operations are privately prepared and fully validated before append. In journaled mode, publication, dependent work, success logs, and committed counters wait for successful append and sync.

| Event | Effects established together |
|---|---|
| `run_started` | Immutable run/account policy; initial cash; zero position; empty identity state |
| `order_admitted` | `NEW` order, intent/order mapping, admission identity, active reservation, committed allocation ordinal |
| `order_submitted` | `SUBMITTED` and immutable original submission context |
| `fill_applied` | Full portfolio mutation, complete reservation release, Fill/settlement identity, Fill allocator advancement |
| `order_filled` | OMS `FILLED`, referencing existing settlement |
| `order_terminal` | Terminal OMS state, outcome identity/reason/context, complete reservation release |
| `intent_denied` | Persistent rejected-intent identity and reason; no order, reservation, or portfolio effect |

For the existing reservation state vocabulary:

- Admission produces `Active`.
- Settlement produces `Settled`, with outcome ID equal to FillID and fixed reason `"FULL_FILL"`.
- Rejection/cancellation produces `Released`, with outcome ID and reason code from the terminal record.

These are derived effects, not additional journal events.

A write/sync failure poisons the writer and halts the run. No retries on that writer, dependent processing, or success summary follow. Recovery considers surviving bytes: a complete valid record may survive an unsuccessful sync acknowledgment.

An absent record leaves prior state. A torn or corrupt record makes the entire recovery fail; no tail repair occurs.

**Crash-prefix walkthrough**

For one BUY of one share, initial cash $1,000, reservation ask $100, execution ask $99, submission N:

| Surviving prefix | OMS | Reservation | Actual cash / position | Execution eligibility |
|---|---|---|---|---|
| `run_started`, before admission | No order | None | $1,000 / 0 | None |
| Admission | `NEW` | Active $100 | $1,000 / 0 | Not submitted |
| Submission | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2 |
| Prospective broker Fill exists only in memory | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2; no durable settlement |
| Settlement | `SUBMITTED` | Settled, $0 active | $901 / 1 | Never executable again |
| `FILLED` | `FILLED` | Settled, $0 active | $901 / 1 | Never executable again |

This assumes each indicated prefix ends cleanly on a record boundary. Any partial following record is fatal.

Execution-rejection alternative, execution ask $101:

| Surviving prefix | OMS | Reservation | Actual cash / position | Execution eligibility |
|---|---|---|---|---|
| Before admission | No order | None | $1,000 / 0 | None |
| Admission | `NEW` | Active $100 | $1,000 / 0 | Not submitted |
| Submission | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2 |
| Rejection calculated only in memory | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2; no durable outcome |
| Terminal rejection | `REJECTED` | Released, $0 active | $1,000 / 0 | Never executable again |

Cancellation alternative, command accepted after N+1:

| Surviving prefix | OMS | Reservation | Actual cash / position | Execution eligibility |
|---|---|---|---|---|
| Before admission | No order | None | $1,000 / 0 | None |
| Admission | `NEW` | Active $100 | $1,000 / 0 | Not submitted; cancellation invalid |
| Submission | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2 |
| Cancellation prepared only in memory | `SUBMITTED` | Active $100 | $1,000 / 0 | N+2; no durable cancellation |
| Terminal cancellation | `CANCELLED` | Released, $0 active | $1,000 / 0 | Never executable again |

Inspection reports these eligibility properties; it does not resume execution.

**5. Recovery-state table**

“Executable” below means lifecycle eligibility for a future engine, not permission for the recovery inspector to execute.

| Durable history | OMS | Portfolio effect | Reservation | Settlement identity | Executable? | Outstanding? | Result |
|---|---|---|---|---|---|---|---|
| Run start only | No orders | Initial account | None | None | No | No | Recoverable |
| Admission only | `NEW` | None | Active | None | No | Yes | Recoverable |
| Admission + submission | `SUBMITTED` | None | Active | None | At N+2 | Yes | Recoverable |
| Settlement, no `FILLED` | `SUBMITTED` | Full Fill once | Settled/released | Retained | No | No | Recoverable |
| Settlement + `FILLED` | `FILLED` | Same full Fill once | Settled/released | Retained | No | No | Recoverable |
| Valid rejection | `REJECTED` | None | Released | None | No | No | Recoverable |
| Valid cancellation | `CANCELLED` | None | Released | None | No | No | Recoverable |
| Risk denial | No order | None | Never acquired | None | No | No | Recoverable; denial retained |
| Impossible lifecycle, identity conflict, invalid arithmetic, corrupt input | No returned state | No returned state | No returned state | No returned state | No | Not reported | Fatal |

Outstanding count includes admitted `NEW` and unsettled `SUBMITTED` orders. It excludes settled orders awaiting OMS finalization.

EOF emits no automatic fill, cancellation, release, or finalization. Recovery reconstructs only durable state. Normal run reporting includes outstanding count, reserved cash, and reserved sell quantity.

No quote mark, strategy state, replay cursor, or automatic resume is inferred from the last trading record.

**6. Phase 1 compatibility policy**

Choose **a version dispatcher with separate semantics**:

- Version 2 uses the existing Phase 1 decoder and recovery rules.
- Version 3 uses this contract.
- Unknown versions fail explicitly.
- Mixed-version journals fail.
- Version selection reads the first record’s explicit version without assuming missing fields or defaults.

Version 2 remains readable but is never interpreted as Phase 2 state. Its missing reservation metadata is **unavailable**, not zero and not inferred from execution prices.

No automatic migration, implicit upgrade, or append of v3 records to a v2 file is permitted. Existing exclusive-create behavior and read-only recovery remain.

**7. Example JSONL records**

The checksums below were calculated over the specified canonical records. Each object occupies one newline-terminated line.

The first five records form one valid BUY history. All quote timestamps deliberately match; sequences still distinguish N=1 from execution at 3.

```jsonl
{"record":{"version":3,"sequence":1,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"run_started","payload":{}}},"crc32c":1757333781}
{"record":{"version":3,"sequence":2,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"order_admitted","payload":{"admission_id":"admission-1","admission_ordinal":1,"order_id":"order-1","intent":{"intent_id":"intent-1","symbol":"XYZ","side":"BUY","quantity":1,"timestamp":"2026-09-16T13:00:00Z"},"origin_quote":{"sequence":1,"timestamp":"2026-09-16T13:00:00Z","bid":990000,"ask":1000000},"status":"NEW","reservation":{"type":"BUY_CASH","reference_ask":1000000,"reserved_money":1000000}}}},"crc32c":2073893609}
{"record":{"version":3,"sequence":3,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"order_submitted","payload":{"order_id":"order-1","admission_id":"admission-1","quote_sequence":1,"timestamp":"2026-09-16T13:00:00Z"}}},"crc32c":1820013472}
{"record":{"version":3,"sequence":4,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"fill_applied","payload":{"fill_id":"fill-1","order_id":"order-1","admission_id":"admission-1","symbol":"XYZ","side":"BUY","quantity":1,"price":990000,"execution_quote":{"sequence":3,"timestamp":"2026-09-16T13:00:00Z","bid":980000,"ask":990000}}}},"crc32c":1773652221}
{"record":{"version":3,"sequence":5,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"order_filled","payload":{"order_id":"order-1","admission_id":"admission-1","fill_id":"fill-1"}}},"crc32c":3145687143}
```

Execution rejection is an **alternative record 4** after the same first three records. It replaces the settlement/finalization path:

```jsonl
{"record":{"version":3,"sequence":4,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"order_terminal","payload":{"outcome_id":"outcome/order-1","order_id":"order-1","admission_id":"admission-1","status":"REJECTED","reason":{"code":"BUY_RESERVATION_EXCEEDED"},"context":{"type":"QUOTE","quote":{"sequence":3,"timestamp":"2026-09-16T13:00:00Z","bid":1000000,"ask":1010000}}}}},"crc32c":2589249732}
```

Cancellation is another **alternative record 4**, accepted between quotes 2 and 3:

```jsonl
{"record":{"version":3,"sequence":4,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"order_terminal","payload":{"outcome_id":"outcome/order-1","order_id":"order-1","admission_id":"admission-1","status":"CANCELLED","reason":{"code":"CANCEL_REQUESTED"},"context":{"type":"CONTROL","command_id":"cancel-1","after_quote_sequence":2}}}},"crc32c":715695919}
```

For a separate denied-intent history, append this as record 2 directly after the run-start record. Eleven shares at $100 require $1,100 against $1,000 cash:

```jsonl
{"record":{"version":3,"sequence":2,"run":{"run_id":"fixture-1","symbol":"XYZ","initial_cash":10000000,"mode":"PAPER","money_scale":10000,"quantity_unit":"WHOLE_SHARE","position_policy":"LONG_ONLY","fill_policy":"FULL_ONLY","fee_policy":"NONE","quote_delay":2,"buy_reservation_policy":"SUBMISSION_ASK_CAP","risk_limits":{"max_order_notional":10000000,"max_position":10}},"event":{"type":"intent_denied","payload":{"denial_id":"denial/intent-2","intent":{"intent_id":"intent-2","symbol":"XYZ","side":"BUY","quantity":11,"timestamp":"2026-09-16T13:00:00Z"},"origin_quote":{"sequence":1,"timestamp":"2026-09-16T13:00:00Z","bid":990000,"ask":1000000},"reason":{"code":"INSUFFICIENT_CASH"}}}},"crc32c":3883005193}
```

**8. QA acceptance matrix**

These are future test requirements, not tests implemented in this ticket.

| Scenario | Required result |
|---|---|
| Valid v3 BUY then SELL histories | Exact accounting, full release, retained identities and correct allocators |
| Run-start-only journal | Recover initial cash, zero position, no orders/reservations |
| Each admission/submission/settlement/finalization crash prefix | Recover exactly the corresponding prefix state |
| Settlement before OMS `FILLED` | Changed portfolio, settled reservation, nonexecutable order |
| Valid legacy v2 journal | Recover through unchanged Phase 1 semantics; no invented reservation metadata |
| Unknown version or mixed versions | Fatal |
| Missing, null, unknown, duplicate, or case-variant field | Fatal, even with recomputed checksum |
| Missing BUY reservation field or SELL carrying BUY fields | Fatal |
| BUY amount inconsistent with reference ask × quantity | Fatal |
| SELL reserved quantity inconsistent with order quantity | Fatal |
| Inconsistent run identity, limits, scale, or policy | Fatal |
| Identical admission retry after release/settlement | Historical result; no new reservation or ordinal |
| Conflicting IntentID, OrderID, AdmissionID, or ordinal | Fatal |
| Submission before admission or changed retry sequence | Fatal |
| Identical settlement retry, including after `FILLED` | Accounting and release occur once |
| Conflicting FillID or second distinct Fill for one order | Fatal |
| Settlement followed by rejection/cancellation | Fatal |
| Settlement after rejection/cancellation | Fatal |
| `FILLED` without settlement | Fatal |
| `NEW → CANCELLED` | Fatal |
| Valid `NEW → REJECTED` | Reservation released, portfolio unchanged |
| Identical terminal retry | No repeated release |
| Conflicting terminal reason, context, status, or command identity | Fatal |
| Same timestamp at N, N+1, N+2 | Distinct sequences; execute only at N+2 |
| Execution early, late, or out of eligible-order order | Fatal |
| BUY cost equal to reservation | Full settlement |
| BUY cost below reservation | Actual cost charged; entire reservation released |
| BUY cost above reservation | Rejection and release; unchanged portfolio |
| Rejected execution retried at cheaper quote | Return original rejection; no Fill |
| Risk-denial retry | Same retained denial; no order/reservation |
| Denial reason inconsistent with replayed risk result | Fatal |
| Truncated/torn line or missing final newline | Fatal; no recovered partial state |
| Checksum corruption | Fatal |
| Journal sequence gap, duplicate, or overflow | Fatal |
| Notional, aggregate, cash-credit, position, or eligibility overflow | Fatal; no returned state |
| Input whitespace/member-order changes with same canonical data | Same checksum interpretation and state |
| Failed sync with complete record surviving versus absent record | Recover the actual complete prefix; no acknowledgment assumptions |
| EOF with active orders | Preserve reservations and report outstanding totals |

**9. Unresolved design questions**

None within this journal/recovery contract.

The additional run-start record, durable denial records, retained quote context, and strict event-specific schemas are explicit v3 choices requiring review approval. They do not authorize implementation, production wiring, a cancellation UI, strategy recovery, or simulation resume.

DESIGN_READY_FOR_REVIEW
