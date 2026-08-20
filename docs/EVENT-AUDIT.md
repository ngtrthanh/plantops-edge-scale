# Edge Scale — Operational Event & Action Audit

Status: Phase 2C design truth.

## Two audit streams, two jobs

Do not combine raw scale telemetry and operational decisions into one noisy file.

```text
RAW WEIGHT AUDIT
  every controller frame, 24/7
  exact bytes + receive UTC + parsed weight/stable/fault
  high volume
  data/raw-weight.jsonl

OPERATIONAL EVENT AUDIT
  meaningful decisions/actions/changes only
  state, identity, faults, overrides, ticket, physical commands/results
  low volume
  data/events.jsonl
```

Both streams are append-only and SHA-256 hash chained using:

```text
seq
prev_hash
event
hash
```

Existing chains are verified at process startup. A broken chain is not silently repaired.

## Event fields

```text
station_id
transaction_id
at_utc
kind
actor
source
device
action
old_state
new_state
reason
evidence
data
runtime_git_sha
```

`runtime_git_sha` ties each action/decision to the exact software build that produced it.

## Workflow event policy

A change-driven watcher records meaningful transitions; it does not log identical snapshots every polling cycle.

Current event classes include:

```text
TRANSACTION_STARTED
STATE_TRANSITION
RFID_OBSERVED
LPR_OBSERVED
IDENTITY_DECISION
POSITION_DECISION
FAULT_SET
FAULT_CLEARED
OVERRIDE_AUTHORIZED
STABLE_WEIGHT_ACCEPTED
TICKET_COMMITTED
DESIRED_OUTPUTS_CHANGED
TRANSACTION_COMPLETED
TRANSACTION_RESET
```

The accepted stable-weight event includes the raw weight `seq/hash`, preserving the bridge:

```text
operational event
→ ticket
→ exact accepted raw weight frame
→ complete raw-weight curve
```

## Physical output audit gate

Physical permissive commands are special. They are not merely observed after the fact.

For these `true`/permissive directions:

```text
ENTRY_BARRIER_OPEN_REQUEST
EXIT_BARRIER_OPEN_REQUEST
ENTRY GREEN
EXIT GREEN
BUZZER ON
```

the runtime requires:

```text
append + fsync OUTPUT_COMMAND intent
→ success
→ execute Modbus command
→ append OUTPUT_RESULT
```

If the intent cannot be durably audited, the physical permissive command is **not executed**.

## Safe commands are never blocked by audit failure

A failed audit disk must never prevent the application from removing permission or returning outputs to a safe state.

Examples:

```text
GREEN OFF
OPEN_REQUEST OFF
BUZZER OFF
SafeState
```

These execute first even if `events.jsonl` is unavailable. Result audit is best-effort. Audit failure still creates a critical `AUDIT_STORE` fault for operator visibility/lockout.

This asymmetry is intentional:

```text
permission requires audit
removing permission does not
```

## Audit-store failure

Operational audit persistence failure is represented as:

```text
device = AUDIT_STORE
health = FAULT
critical = true
overridable = false
```

An active transaction therefore enters `FAULT_LOCKOUT` rather than continuing with unaudited permissive actions.

## APIs

```text
GET /api/audit/events?limit=200
GET /api/audit/events/verify
```

The replay endpoint returns:

```json
{
  "count": 2,
  "records": [
    {"seq": 101, "prev_hash": "...", "event": {}, "hash": "..."},
    {"seq": 102, "prev_hash": "...", "event": {}, "hash": "..."}
  ]
}
```

## Audit examples

### State transition

```json
{
  "kind": "STATE_TRANSITION",
  "transaction_id": "tx-...",
  "source": "WORKFLOW",
  "old_state": "IDENTIFYING",
  "new_state": "ENTRY_AUTHORIZED"
}
```

### Stable weight accepted

```json
{
  "kind": "STABLE_WEIGHT_ACCEPTED",
  "source": "SCALE",
  "device": "SCALE",
  "data": {
    "weight_kg": 28460,
    "raw_seq": 18292,
    "raw_hash": "..."
  }
}
```

### Barrier command

```json
{
  "kind": "OUTPUT_COMMAND",
  "source": "RUNTIME_IO",
  "device": "EXIT_BARRIER",
  "action": "EXIT_BARRIER_OPEN_REQUEST",
  "data": {"value": true, "phase": "intent"}
}
```

## CI acceptance

Windows CI verifies:

```text
normal truck workflow
→ event audit contains transaction/state/identity/position/stable/ticket/completion
→ event hash chain verifies

real Modbus simulator workflow
→ entry/exit barrier OPEN intents exist before physical command
→ output results exist
→ every intent/result belongs to the transaction when active
→ event hash chain verifies
```

Unit tests separately verify:

```text
audit disk failure + OPEN=true
→ physical command call count remains zero

audit disk failure + OPEN=false
→ safe physical command still executes
```

## SQLite migration

Phase 3 may replace JSONL with SQLite, but it must preserve these semantics:

```text
ordered append identity
hash chain / tamper evidence
no arbitrary UPDATE/DELETE through normal app API
synchronous durability for permissive command intent
ticket/raw-weight/event cross references
```
