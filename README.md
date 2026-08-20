# PlantOps Edge Scale

Offline-first unmanned truck weighbridge Edge controller.

## Direction

The target runtime is now **Go single binary**:

```text
Windows
└── plantops-edge-scale.exe
    ├── embedded HTTP/API
    ├── embedded operator UI
    ├── domain state machine
    ├── hardware adapters
    ├── local durable persistence
    ├── audit/events
    └── Central sync
```

No IIS. No Docker Desktop. No WSL2. No separate Node runtime. No language runtime bundle.

The existing C# / Kestrel demo remains in the repository temporarily as a validated behavioral reference during migration. It is not the long-term architecture.

## Design truth

Read these first:

- `docs/EDGE-KNOWLEDGE.md` — operating rules, degraded/manual/lockout semantics, override policy.
- `docs/PSEUDOCODE.md` — full repository behavior from process startup through truck cycle, override, persistence, sync, recovery, and deployment.
- `docs/RAW-WEIGHT-AUDIT.md` — mandatory all-frame raw scale journal, time-series reconstruction and integrity rules.
- `docs/HARDWARE-WIRING.md` — hardware topology, signal map, wiring/commissioning/fault-injection plan.
- `docs/GO-PORTING.md` — staged C# → Go cutover plan.

## Non-negotiable safety/operations rules

```text
Scale weight/stable authority: NEVER software-overridden.
Every scale frame: raw bytes + UTC timestamp durably journaled before business use.
Raw audit persistence failure: reading cannot become ticket truth.
Auxiliary sensor override: transaction-scoped only.
One allowed auxiliary failure: DEGRADED if fallback evidence is sufficient.
Multiple correlated failures: MANUAL/supervisor path.
Unsafe critical failure: FAULT_LOCKOUT.
Local durable ticket commit: required before truck release.
Central/WAN outage: must not stop a valid local weighing operation.
Physical barrier safety: independent of application process.
```

## Go vNext currently implemented

```text
cmd/edge/main.go
internal/domain/          health/mode/ticket/override + raw-weight event model
internal/ports/           hardware/persistence boundaries
internal/adapters/
  modbustcp/              dependency-free Modbus TCP DI/coil client
  scaleascii/             generic parser + persistent TCP all-frame collector
  rawjournal/             durable append-only hash-chained raw weight JSONL journal
  ingress/                RFID + LPR normalized webhook ingress
  jsonl/                  durable zero-dependency bootstrap ticket store
internal/httpapi/          embedded API + embedded web UI
```

The live scale collector keeps the TCP connection open, journals every received frame before publishing the parsed reading, journals transport errors, and reconnects without resetting the station audit sequence.

The generic scale parser is intentionally not presented as the production scale protocol. A vendor-specific adapter must be written from the real controller manual/sample frames.

## Go development

```bash
go test ./...
go vet ./...
go run ./cmd/edge
```

Run with live scale collection:

```text
plantops-edge-scale.exe \
  -station-id WHD-NC \
  -scale-addr 192.168.1.50:4001 \
  -raw-weight-journal data/raw-weight.jsonl
```

Open:

```text
http://127.0.0.1:8080
```

Current APIs:

```text
POST /io/rfid
POST /io/lpr
GET  /api/identity
GET  /api/scale/status
GET  /api/audit/weights?limit=200
GET  /api/audit/weights/verify
GET  /healthz
```

## Raw weight audit path

```text
scale controller TCP stream
→ exact controller frame bytes
→ received_at_utc
→ append-only hash-chained raw journal
→ parse weight/stable/fault
→ publish normalized reading
→ future state machine
```

Raw logging is station-continuous. Frames outside an active ticket are still retained; when the state engine exists it will attach the active `transaction_id` without changing the continuous station sequence.

Existing journal integrity is verified at process startup. A broken hash chain is not silently repaired.

## CI

`.github/workflows/build-go-vnext.yml` runs tests/vet on a GitHub-hosted Windows runner, builds one Windows executable, launches a TCP scale simulator, runs the EXE against that socket, and verifies:

```text
/healthz
RFID/LPR ingress
continuous scale collector
1200 → 28420 → 28460 kg raw timeline
stable=true on final frame
exact raw bytes retained
station ID retained
hash-chain verification
live scale status
```

It then hashes the EXE and uploads an immutable artifact.

Target artifact:

```text
plantops-edge-scale.exe
plantops-edge-scale.exe.sha256
```

## Hardware boundary

Preferred architecture:

```text
scale controller ─────────────── TCP/serial adapter ─┐
Modbus TCP remote I/O ─ sensors/outputs ────────────┤
RFID Ethernet reader ───────────────────────────────┤
LPR camera / edge OCR ──────────────────────────────┤
                                                     ↓
                                            Go domain engine
```

The Edge app coordinates business workflow. It does not replace certified weighing authority or the barrier's physical anti-collision/safety circuit.

## Migration status

```text
Phase 0  C# behavioral reference                DONE
Phase 1  Go skeleton + adapter boundaries       BASELINE DONE
Phase 1A raw weight audit model/journal         DONE
Phase 1B continuous raw collector + runtime API IN VERIFICATION
Phase 2  full Go event/state machine            NEXT
Phase 3  SQLite persistence                     NEXT
Phase 4  real scale-controller adapter          NEED PROTOCOL
Phase 5  remote-I/O bench integration           PLANNED
Phase 6  RFID/LPR real integration              PLANNED
Phase 7  production shadow mode                 PLANNED
Phase 8  supervised outputs                     PLANNED
Phase 9  Go primary / C# retired                PLANNED
```
