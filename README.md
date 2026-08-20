# PlantOps Edge Scale

Offline-first unmanned truck weighbridge Edge controller.

## Target runtime

**Go single binary**:

```text
Windows
└── plantops-edge-scale.exe
    ├── embedded HTTP/API + operator UI
    ├── audited truck workflow state engine
    ├── scale / Modbus / RFID / LPR adapters
    ├── local durable ticket + audit persistence
    └── later Central sync
```

No IIS, Docker Desktop, WSL2, Node runtime, or .NET runtime dependency.

The C# implementation remains only as a temporary behavioral reference during migration.

## Design truth

- `docs/EDGE-KNOWLEDGE.md` — operating modes, overrides, lockout and safety rules.
- `docs/PSEUDOCODE.md` — whole-repo behavior.
- `docs/RAW-WEIGHT-AUDIT.md` — mandatory all-frame scale audit.
- `docs/HARDWARE-WIRING.md` — physical topology and commissioning plan.
- `docs/MODBUS-RUNTIME.md` — Phase 2B polling/reconciliation and safe-start semantics.
- `docs/GO-PORTING.md` — staged C# → Go cutover.

## Non-negotiable rules

```text
Scale weight/stable/fault authority: NEVER software-overridden.
Every scale frame: durably journal exact raw bytes + UTC before business use.
Ticket accepted weight: stores exact raw audit seq/hash reference.
Raw audit failure: reading cannot become ticket truth.
Entry: requires RFID/LPR identity, physical safety, clear position sensors,
       and audited HEALTHY + STABLE near-zero scale proof.
Auxiliary override: transaction-scoped only.
Multiple correlated failures: MANUAL/supervisor path.
Critical/unsafe failure: FAULT_LOCKOUT.
Local durable ticket commit: required before truck release.
Central/WAN: never part of local release authorization.
Physical barrier anti-collision/safety: independent of application process.
```

## Implemented Go architecture

```text
cmd/edge/main.go

internal/domain/
  health / modes / faults / overrides
  workflow state / desired outputs
  audited scale reading + raw reference

internal/engine/
  deterministic truck workflow

internal/adapters/
  scaleascii/     persistent audited scale TCP stream
  rawjournal/     append-only SHA-256 chained raw weight journal
  modbustcp/      dependency-free Modbus TCP DI/coil adapter
  ingress/        RFID + LPR normalized ingress
  registry/       bootstrap RFID -> plate map
  jsonl/          durable bootstrap ticket store

internal/runtimeio/
  Modbus input poller
  safe-start/reconnect output reconciler
  barrier-feedback GREEN gating
  bounded buzzer pulse
  remote-I/O/barrier fault reporting

internal/httpapi/
  embedded HTTP API + UI
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

Development:

```bash
go test ./...
go vet ./...
go run ./cmd/edge
```

Scale-only example:

```text
plantops-edge-scale.exe \
  -station-id WHD-NC \
  -scale-addr 192.168.1.50:4001 \
  -raw-weight-journal data/raw-weight.jsonl \
  -vehicle-map "RFID001=15C-123.45"
```

Scale + remote I/O example:

```text
plantops-edge-scale.exe \
  -station-id WHD-NC \
  -scale-addr 192.168.1.50:4001 \
  -io-addr 192.168.1.60:502 \
  -io-unit-id 1 \
  -io-map "safety_clear=8" \
  -raw-weight-journal data/raw-weight.jsonl \
  -ticket-journal data/tickets.jsonl \
  -vehicle-map "RFID001=15C-123.45"
```

`-io-addr` empty means no physical Modbus reads or writes.

## APIs

```text
GET  /healthz
GET  /api/workflow
GET  /api/scale/status
GET  /api/io/status
GET  /api/identity
GET  /api/audit/weights?limit=200
GET  /api/audit/weights/verify
POST /io/rfid
POST /io/lpr
```

`/sim/*` endpoints exist only when the process is started with explicit `-simulation`.

## Raw weight audit

```text
controller frame
→ exact bytes + Edge UTC timestamp
→ fsync append-only hash-chain journal
→ parse/normalize
→ AuditedScaleReading{reading, raw_seq, raw_hash}
→ workflow
→ durable ticket stores accepted raw_seq/raw_hash
```

Therefore an accepted ticket can be traced directly back to the exact controller frame and the complete raw weight curve around that transaction.

## Remote-I/O runtime safety

```text
connect/read remote I/O
→ force SafeState first
   RED, GREEN off, buzzer off, OPEN requests off
→ then reconcile Engine DesiredOutputs
```

A reconnect never blindly replays stale barrier commands.

Permissive GREEN is feedback-gated:

```text
Engine authorizes OPEN + GREEN
→ OPEN_REQUEST coil
→ wait physical OPEN feedback
→ GREEN ON
```

Remote-I/O transport failure or contradictory/timeout barrier feedback is a critical fault.

## CI

GitHub-hosted Windows CI verifies:

```text
go test ./...
go vet ./...
single plantops-edge-scale.exe build
raw-weight TCP simulator
all-frame audit + hash chain
full audited transaction
exact ticket -> raw frame linkage
Modbus TCP I/O simulator
ENTRY sensor -> barrier request -> OPEN feedback -> GREEN
FRONT/REAR position -> audited stable weight -> durable ticket
EXIT barrier -> feedback -> exit sensor -> COMPLETE
no permissive applied outputs after completion
artifact SHA-256
```

Test simulators are never shipped in the production artifact.

## Migration status

```text
Phase 0   C# behavioral reference                         DONE
Phase 1   Go skeleton + adapter boundaries                DONE
Phase 1A  all-frame raw weight audit                      DONE
Phase 2   audited Go truck workflow state engine          DONE
Phase 2B  Modbus input poller + output reconciler         IN VERIFICATION
Phase 2C  general event/action audit journal              NEXT
Phase 3   SQLite single-file persistence                  NEXT
Phase 4   exact real scale-controller adapter             NEED REAL PROTOCOL
Phase 5   real remote-I/O bench integration               PLANNED
Phase 6   real RFID/LPR integration                       PLANNED
Phase 7   production shadow mode                          PLANNED
Phase 8   supervised outputs                              PLANNED
Phase 9   Go primary / C# retired                         PLANNED
```
