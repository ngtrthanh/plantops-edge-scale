# PlantOps Edge Scale

Offline-first unmanned truck weighbridge Edge controller.

## Runtime

```text
Windows
└── plantops-edge-scale.exe
    ├── embedded HTTP/API + operator UI
    ├── deterministic audited truck workflow
    ├── scale / Modbus / RFID / LPR adapters
    ├── embedded pure-Go SQLite business persistence
    └── durable local audit + Central sync queue
```

Production target: **one Go EXE**. No IIS, Docker Desktop, WSL2, Node, .NET runtime, PostgreSQL service, CGO, or SQLite DLL.

Persistent files:

```text
data/edge.db             tickets + overrides + sync queue + station state
 data/raw-weight.jsonl   all controller frames / exact bytes / hash chain
 data/events.jsonl       decisions + overrides + faults + output command/result hash chain
```

The independent audit journals are intentional: forensic evidence remains directly inspectable and hash-verifiable even if relational storage is unavailable.

## Design truth

- `docs/EDGE-KNOWLEDGE.md` — operating modes, overrides, lockout and safety rules.
- `docs/PSEUDOCODE.md` — whole-repo behavior.
- `docs/RAW-WEIGHT-AUDIT.md` — all-frame scale audit.
- `docs/EVENT-AUDIT.md` — operational/action audit gate.
- `docs/HARDWARE-WIRING.md` — topology and commissioning plan.
- `docs/MODBUS-RUNTIME.md` — Modbus polling/reconciliation and safe-start semantics.
- `docs/SQLITE-PERSISTENCE.md` — SQLite durability, recovery and sync queue contract.
- `docs/GO-PORTING.md` — C# → Go cutover record.

## Non-negotiable rules

```text
Scale weight/stable/fault authority: NEVER software-overridden.
Every scale frame: exact bytes + Edge UTC durably journaled before business use.
Accepted ticket: stores exact raw audit seq/hash.
Raw audit failure: reading cannot become ticket truth.
Permissive physical output: durable event intent before Modbus execution.
Audit failure: permissive action blocked; safe/off action remains allowed.
Entry: identity + physical safety + clear deck + audited stable near-zero scale.
Auxiliary override: transaction-scoped evidence only.
Critical/unsafe failure: FAULT_LOCKOUT.
Ticket + override + sync queue: one local SQLite transaction.
Local durable commit: required before EXIT_AUTHORIZED.
Central/WAN: never part of local truck-release authorization.
Physical barrier anti-collision/safety: independent of the application process.
Restart: SafeState first, new engine IDLE, never replay stale permissive command.
```

## Code map

```text
cmd/edge/main.go

internal/domain/          workflow/audit/fault/override contracts
internal/engine/          deterministic truck state machine
internal/workflowaudit/   synchronous before/after operational audit recorder

internal/adapters/
  scaleascii/             persistent scale TCP collector/parser boundary
  rawjournal/             all-frame raw-weight SHA-256 chain
  auditjournal/           operational SHA-256 chain
  sqlitestore/            edge.db TicketStore + sync queue + integrity/recovery
  modbustcp/              dependency-free Modbus TCP DI/coil adapter
  ingress/                RFID/LPR normalized ingress
  registry/               bootstrap RFID -> plate map

internal/runtimeio/       safe-start poller + desired-output reconciler + audit gate
internal/httpapi/         embedded HTTP API + UI
```

## Truck workflow

```text
IDLE
→ APPROACH
→ IDENTIFYING
→ ENTRY_AUTHORIZED
→ ENTERING
→ POSITIONING
→ READY_TO_WEIGH
→ WEIGHING
→ LOCAL_COMMITTED
→ EXIT_AUTHORIZED
→ EXITING
→ COMPLETE
```

Critical failure can transition the active transaction to `FAULT_LOCKOUT` at any point.

## Run

```text
plantops-edge-scale.exe \
  -station-id WHD-NC \
  -db data/edge.db \
  -scale-addr 192.168.1.50:4001 \
  -io-addr 192.168.1.60:502 \
  -io-unit-id 1 \
  -io-map "safety_clear=8" \
  -raw-weight-journal data/raw-weight.jsonl \
  -event-journal data/events.jsonl \
  -vehicle-map "RFID001=15C-123.45"
```

`-io-addr` empty means no physical Modbus reads/writes. `/sim/*` exists only with explicit `-simulation`.

## APIs

```text
GET  /healthz
GET  /api/workflow
GET  /api/scale/status
GET  /api/io/status
GET  /api/storage/status
GET  /api/identity
GET  /api/audit/weights?limit=200
GET  /api/audit/weights/verify
GET  /api/audit/events?limit=200
GET  /api/audit/events/verify
POST /io/rfid
POST /io/lpr
```

## Evidence path

```text
controller frame bytes
→ raw-weight fsync + seq/hash
→ AuditedScaleReading
→ workflow stable acceptance
→ SQLite atomic ticket commit
   ├── ticket exact raw seq/hash
   ├── override evidence
   ├── station pointer
   └── pending Central sync item
→ EXIT_AUTHORIZED
```

Physical output path:

```text
Engine DesiredOutputs
→ runtime reconciler
→ durable OUTPUT_COMMAND intent
→ Modbus command
→ physical feedback
→ OUTPUT_RESULT audit
```

GREEN is physically feedback-gated and only illuminates after barrier OPEN feedback.

## SQLite policy

```text
WAL
synchronous=FULL
foreign_keys=ON
busy_timeout=5000
startup integrity_check
single database/sql connection
```

Central outage leaves `sync_queue` pending but does not block a valid local transaction.

## Windows CI acceptance

GitHub-hosted Windows CI runs:

```text
go mod tidy
go test ./...
go vet ./...
single EXE build
TCP scale simulator
all-frame raw audit + exact ticket linkage
operational hash-chain audit
Modbus TCP remote-I/O simulator
barrier request -> OPEN feedback -> GREEN
full truck cycle -> 28460 kg -> SQLite commit -> COMPLETE
SQLite tickets=1 + pending_sync=1 + integrity=ok
process stop + reopen same edge.db
reboot state IDLE + durable ticket/queue retained + no I/O replay
artifact SHA-256
```

Test simulators are not shipped in the production artifact.

## Status

```text
Phase 0   C# behavioral reference                         DONE
Phase 1   Go skeleton + hardware boundaries               DONE
Phase 1A  all-frame raw-weight audit                      DONE
Phase 2   audited Go truck workflow engine                DONE
Phase 2B  Modbus poller + output reconciler               DONE
Phase 2C  operational/action audit + permissive gate      DONE
Phase 3   pure-Go SQLite local durability                 IN VERIFICATION
Phase 4   exact scale-controller vendor adapter           NEED REAL PROTOCOL
Phase 5   real remote-I/O bench integration               NEED HARDWARE
Phase 6   real RFID/LPR integration                       NEED DEVICE API
Phase 7   production shadow mode                          SITE STEP
Phase 8   supervised physical outputs                     SITE STEP
Phase 9   Go primary / C# retired                         SITE CUTOVER
```

The remaining items after Phase 3 require the actual scale-controller protocol and physical devices/site commissioning; they cannot be truthfully completed in simulation alone.
