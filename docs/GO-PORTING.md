# Edge Scale — C# to Go Porting Plan

## Decision

Target runtime is Go, one executable.

```text
plantops-edge-scale.exe
├── domain state machine
├── hardware adapters
├── embedded HTTP server/API
├── embedded web UI
├── local persistence
├── audit/event journal
├── Central sync
└── heartbeat/deployment identity
```

The current C# app is retained only as a working behavioral reference until Go reaches parity.

## Why Go here

The Edge controller is mostly TCP/serial I/O, HTTP, Modbus TCP, a state machine, a small local DB, background loops, audit/sync, and an embedded static UI. Go maps directly to this problem while preserving a small operational surface and a single deployable binary.

## Non-goals

Do not translate `Program.cs` line-by-line. Do not reproduce ASP.NET structure in Go. Do not introduce microservices on one scale PC. Do not add a separate Node frontend runtime. Do not add Docker as a runtime dependency.

## Target repository shape

```text
cmd/edge/main.go

internal/domain/
  types.go
  policy.go
  engine.go
  state_machine.go
  override.go

internal/ports/
  ports.go

internal/adapters/
  modbustcp/
  scaleascii/
  scale_<vendor>/
  ingress/
  rfid_<vendor>/
  lpr_axis/
  sqlite/
  central/

internal/httpapi/
  server.go
  web/

docs/
  EDGE-KNOWLEDGE.md
  PSEUDOCODE.md
  HARDWARE-WIRING.md
  GO-PORTING.md
```

## Migration phases

### Phase 0 — freeze C# behavior

Treat the existing validated demo as reference only. Capture acceptance tests for normal truck cycle, RFID/LPR mismatch, scale disconnect, one position sensor failure, both position sensors failure, exit sensor failure, Central outage, override authorization/expiry, and process restart.

### Phase 1 — Go skeleton and adapters

Started on `go-vnext`:

```text
ports/interfaces
domain health/mode/override policy
generic TCP ASCII scale adapter skeleton
minimal Modbus TCP client + remote-I/O adapter
RFID webhook ingress
LPR webhook ingress
durable JSONL bootstrap store
embedded HTTP server/UI
Go CI single-exe build
```

Goal: prove clean dependency boundaries and Windows single-binary operation.

### Phase 2 — full domain state machine

Implement the repository pseudocode as explicit transitions.

Recommended pattern:

```text
Observation/Event
      ↓
Engine.Apply(event)
      ↓
new domain state
      ↓
commands[]
      ↓
adapters execute commands
      ↓
feedback events return to engine
```

This makes simulation and fault injection straightforward.

### Phase 3 — persistence to SQLite

Replace JSONL bootstrap store with SQLite behind the same ports.

Required tables:

```text
transactions
tickets
evidence
hardware_events
audit_events
overrides
sync_queue
deployment_receipts
heartbeats
```

Migration must remain one binary + one DB file. Prefer a pure-Go SQLite driver if operationally acceptable; validate performance, locking, backup, and binary size before choosing the final driver.

### Phase 4 — real scale adapter

Obtain exact controller protocol manual/sample frames. Write a vendor-specific parser with golden-frame tests.

Acceptance:

```text
100% agreement with physical display on test set
stable transition correct
overload/fault recognized
disconnect recognized
malformed frame rejected
no synthetic stable signal
```

### Phase 5 — real remote I/O

Wire one selected Modbus TCP I/O module on bench. Validate DI read, input filtering, output commands, fail/reconnect behavior, and channel mapping.

### Phase 6 — RFID/LPR

Keep normalized ingress contracts stable:

```json
POST /io/rfid
{"tag":"...","quality":99}
```

```json
POST /io/lpr
{"plate":"...","confidence":98,"image_ref":"..."}
```

Vendor adapters translate native events into these domain observations.

### Phase 7 — shadow mode

Run Go beside the current WSM/C# runtime without controlling barriers.

```text
real inputs
→ current production flow
→ Go shadow observer
```

Compare identity, position decisions, stable weights, transaction timing, and fault classification. No actuator writes from Go in shadow mode.

### Phase 8 — supervised output mode

Enable Go outputs in this order:

```text
lights
→ buzzer
→ entry barrier
→ exit barrier
```

Physical safety remains independent.

### Phase 9 — Go becomes primary

Acceptance gate:

```text
normal cycle pass
all fault-injection cases pass
no unresolved transaction divergence
reboot recovery pass
Central offline pass
operator override audit pass
rollback package prepared
```

Then freeze C# and remove .NET runtime from the Edge deployment.

## Build/release target

```text
GOOS=windows
GOARCH=amd64
CGO_ENABLED=0 where possible
-trimpath
-ldflags "-s -w -X main.version=... -X main.gitSHA=..."
```

Artifact:

```text
plantops-edge-scale.exe
plantops-edge-scale.exe.sha256
config.example.json
install-service.ps1
```

Do not bundle language/runtime files.

## Windows service

Preference: native Go Windows Service integration so deployment remains one application binary plus config/data. Use a service wrapper only if it provides a clear operational benefit.

## Definition of done

Go port is done when C# can be removed without losing workflow behavior, hardware integration, degraded/override semantics, local durability, auditability, Central sync, deployment receipt/heartbeat, operator UI, and fault-injection coverage.

The success metric is not fewer source lines. It is a smaller, clearer operational system with one runtime artifact.
