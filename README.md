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
- `docs/HARDWARE-WIRING.md` — hardware topology, signal map, wiring/commissioning/fault-injection plan.
- `docs/GO-PORTING.md` — staged C# → Go cutover plan.

## Non-negotiable safety/operations rules

```text
Scale weight/stable authority: NEVER software-overridden.
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
internal/domain/          health/mode/ticket/override policy
internal/ports/           hardware/persistence boundaries
internal/adapters/
  modbustcp/              dependency-free Modbus TCP DI/coil client
  scaleascii/             generic TCP scale transport/parser skeleton
  ingress/                RFID + LPR normalized webhook ingress
  jsonl/                  durable zero-dependency bootstrap ticket store
internal/httpapi/          embedded API + embedded web UI
```

The generic scale parser is intentionally not presented as the production scale protocol. A vendor-specific adapter must be written from the real controller manual/sample frames.

## Go development

```bash
go test ./...
go vet ./...
go run ./cmd/edge
```

Open:

```text
http://127.0.0.1:8080
```

Current normalized identity ingress:

```text
POST /io/rfid
POST /io/lpr
GET  /api/identity
GET  /healthz
```

## CI

`.github/workflows/build-go-vnext.yml` runs tests/vet on a GitHub-hosted Windows runner, builds one Windows executable, launches it, verifies `/healthz`, injects RFID/LPR events, verifies normalized identity output, hashes the EXE, and uploads an immutable artifact.

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
Phase 1  Go skeleton + adapter boundaries       IN PROGRESS
Phase 2  full Go event/state machine            NEXT
Phase 3  SQLite persistence                     NEXT
Phase 4  real scale-controller adapter          NEED PROTOCOL
Phase 5  remote-I/O bench integration           PLANNED
Phase 6  RFID/LPR real integration              PLANNED
Phase 7  production shadow mode                 PLANNED
Phase 8  supervised outputs                     PLANNED
Phase 9  Go primary / C# retired                PLANNED
```
