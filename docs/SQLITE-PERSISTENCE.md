# SQLite Edge Persistence

## Runtime shape

```text
plantops-edge-scale.exe
├── edge.db                 durable business state
├── raw-weight.jsonl        all controller frames / hash chain
└── events.jsonl            operational decisions/actions / hash chain
```

SQLite is embedded through the pure-Go `modernc.org/sqlite` driver. Production does not require PostgreSQL, a database Windows Service, CGO, a SQLite DLL, Docker, IIS, WSL2, or a language runtime.

## Durability boundary

`TicketStore.Commit()` is the workflow release boundary. One SQLite transaction persists:

```text
ticket
+ exact accepted raw-weight seq/hash
+ override evidence
+ last_committed_ticket station pointer
+ pending Central sync queue item
```

Only after that transaction commits can the domain engine move to `EXIT_AUTHORIZED`.

## Database policy

At open:

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=FULL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA wal_autocheckpoint=1000;
PRAGMA integrity_check;
```

The Go process uses one SQLite connection. This is deliberate: the edge workload is small, write ordering matters more than database-level parallelism, and a single connection ensures connection-scoped PRAGMAs are consistent.

## Schema v1

```text
tickets
  immutable ticket evidence + normalized columns

overrides
  normalized override evidence linked to ticket

sync_queue
  durable outbound work; Central outage does not affect local release

station_state
  small durable station pointers / recovery metadata
```

Raw scale frames remain outside SQLite because they are a continuous high-volume forensic stream with their own exact-byte hash chain. Operational events also retain their independent append-only hash chain. This keeps evidence independently verifiable even if the relational database is unavailable.

## Restart semantics

A process/Windows restart never resumes an old in-memory state machine and never replays old permissive output commands.

```text
process starts
→ verify raw-weight chain
→ verify operational-event chain
→ open edge.db
→ SQLite integrity_check
→ runtime I/O establishes SafeState
→ new Engine starts IDLE
→ existing tickets + pending sync remain durable
```

An interrupted transaction that never crossed local durable ticket commit is not silently converted into a valid ticket after restart. Its raw/event evidence remains available for investigation.

## Central synchronization

A successful local ticket commit inserts one `TICKET` row into `sync_queue` in the same transaction. A future/connected Central worker may retry independently.

```text
Central offline
→ ticket still commits locally
→ truck may release after all local conditions pass
→ sync_queue remains pending
→ later retry
→ ACK marks queue row + ticket synced_at atomically
```

Central is never in the physical truck-release critical path.

## Backup

For a stopped application, copying `edge.db` after a WAL checkpoint is sufficient. For online backup, use a SQLite-aware backup mechanism rather than copying only the main DB while WAL may contain committed pages.

Before schema-changing deployment:

```text
1. stop new transactions
2. checkpoint WAL
3. backup edge.db
4. deploy
5. run migration + integrity_check
6. health check exact Git SHA
7. rollback binary + DB backup if migration cannot be reversed
```
