# Raw Weight Audit Journal

Status: mandatory design truth for production Edge Scale.

## Why

A final ticket weight is not enough to audit a weighing transaction. The Edge must retain the controller observations that led to that ticket so a later investigation can reconstruct the time series and verify exactly when the controller reported unstable/stable/fault states.

## Non-negotiable rule

```text
EVERY scale controller frame received by Edge
→ timestamp immediately in UTC
→ preserve exact raw bytes
→ parse/normalize
→ durably append to raw weight journal
→ only then expose the parsed reading to business logic
```

If durable raw-journal append fails, the reading is not eligible to become ticket truth. New automatic ticket commit must be blocked until audit persistence is healthy again.

## Event fields

```text
station_id
transaction_id           optional before/after an active transaction
kind                     FRAME | TRANSPORT_ERROR
received_at_utc
source                    controller address / adapter identity
raw_base64                exact bytes as received
raw_text                  convenience view for textual protocols only
weight_kg                 parsed value when parse succeeds
stable                    parsed authoritative stable bit/state
overload                  parsed when available
fault                     controller fault when available
health
parse_ok
error
```

The raw bytes are the primary protocol evidence. `raw_text` is never more authoritative than `raw_base64`.

## Journal envelope

Bootstrap JSONL storage adds:

```text
seq
prev_hash
hash
```

Each row SHA-256 chains to the previous row. This is tamper-evident, not a substitute for access control or backups. The later SQLite implementation must preserve the same ordered append-only semantics and provide integrity verification.

## What must be logged

Do not log only stable readings and do not downsample before the audit journal.

Log:

```text
all valid weight frames
all unstable frames
all stable frames
zero / near-zero frames
weight transitions
controller overload/fault frames
malformed frames
partial frames when a transport read fails
connect/read timeout or disconnect events
recovery/reconnect events when the adapter loop is implemented
```

Business analytics may downsample a separate derived series; the raw audit stream is never derived from the downsampled data.

## Time semantics

`received_at_utc` is assigned by Edge when the complete frame (or failed/partial read) is observed. If a future controller protocol contains its own controller timestamp, store that separately; do not replace Edge receive time.

Time synchronization of the Edge PC is therefore operationally important. NTP/Windows Time health should later be included in station health/audit.

## Transaction linkage

Raw samples are continuous and may exist without a transaction. When a transaction is active, append its `transaction_id` to each event so later audit can query:

```text
transaction
→ accepted ticket
→ exact raw weight curve before and after stable acceptance
```

Recommended audit window for UI/export is the entire transaction plus configurable context before approach and after completion.

## Retention

Keep the raw journal locally long enough for operational investigation and sync/archive it according to site policy. Do not delete data merely because the ticket has synchronized to Central.

At high controller sample rates, use rotation/compression/archive rather than sampling away evidence.

## Production SQLite target

Recommended logical table:

```sql
raw_weight_event(
  seq INTEGER PRIMARY KEY,
  station_id TEXT,
  transaction_id TEXT,
  kind TEXT NOT NULL,
  received_at_utc TEXT NOT NULL,
  source TEXT NOT NULL,
  raw_blob BLOB,
  weight_kg INTEGER,
  stable INTEGER,
  overload INTEGER,
  fault TEXT,
  health TEXT NOT NULL,
  parse_ok INTEGER NOT NULL,
  error TEXT,
  prev_hash BLOB,
  hash BLOB NOT NULL
)
```

Useful indexes:

```text
(received_at_utc)
(transaction_id, received_at_utc)
```

## Acceptance tests

```text
send unstable→stable frame sequence
→ every frame appears in order
→ exact raw bytes recoverable
→ timestamps monotonic by receive order
→ final stable ticket can be traced to its raw event

send malformed frame
→ raw bytes still journaled
→ parse_ok=false
→ ticket logic does not accept it

kill scale TCP connection
→ transport error journaled

make journal path unwritable/full
→ scale reading cannot be accepted for ticket commit

modify/delete/reorder a JSONL row
→ Verify() fails hash-chain validation

restart Edge
→ journal sequence/hash chain resumes, not resets
```
