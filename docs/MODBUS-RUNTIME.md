# Edge Scale — Modbus Runtime I/O

Status: Phase 2B design truth.

## Boundary

The workflow engine never writes Modbus directly.

```text
Engine
  └── DesiredOutputs
          ↓
    runtimeio.Controller
      ├── polls PositionIO
      ├── reconciles desired vs applied outputs
      ├── validates barrier feedback
      └── reports faults back to Engine
          ↓
      modbustcp.IO
          ↓
      remote I/O
```

This separation allows the state machine to remain deterministic and testable while physical I/O handling owns transport, feedback, retries and safe startup.

## Startup / reconnect rule

Before any workflow output is honored:

```text
read remote I/O successfully
→ force SafeState
   entry GREEN OFF
   entry RED ON
   exit GREEN OFF
   exit RED ON
   buzzer OFF
   entry OPEN_REQUEST OFF
   exit OPEN_REQUEST OFF
→ only then resume reconciliation
```

If communication is lost, `safeApplied` is invalidated. After reconnect the same SafeState sequence is mandatory before any remembered workflow desire can be applied.

This prevents a stale `OPEN` desire from being blindly replayed immediately after Windows/network/remote-I/O recovery.

## Permissive-light rule

A domain authorization is not enough to light GREEN.

```text
Engine wants ENTRY_OPEN + ENTRY_GREEN
→ apply ENTRY_OPEN_REQUEST
→ wait for ENTRY_BARRIER_OPEN_FB
→ only then ENTRY_GREEN ON
```

Same for exit.

If OPEN feedback does not arrive before the configured timeout, the corresponding barrier is faulted as critical and the active workflow moves to `FAULT_LOCKOUT`.

## Remote-I/O fault rule

Read or write failure:

```text
REMOTE_IO = DISCONNECTED/FAULT
critical = true
overridable = false
→ active transaction FAULT_LOCKOUT
```

The controller keeps polling so operator diagnostics can show recovery, but an already locked transaction is not silently auto-unlocked.

## Barrier feedback consistency

These combinations are invalid:

```text
ENTRY_BARRIER_OPEN_FB = true
AND ENTRY_BARRIER_CLOSED_FB = true

EXIT_BARRIER_OPEN_FB = true
AND EXIT_BARRIER_CLOSED_FB = true
```

They create a critical barrier fault.

## Buzzer semantics

`DesiredOutputs.Buzzer=true` is interpreted as a release event, not a continuously energized output.

The reconciler detects the false→true edge and creates one bounded pulse (`-io-buzzer-pulse`, default 700 ms). Keeping the desired flag true does not repeatedly retrigger the buzzer.

## Input polling

All configured DI channels are read as one contiguous Modbus block from minimum to maximum address. Do not open one TCP request per sensor.

Default logical DI map:

```text
0 ENTRY_PRESENT
1 FRONT_PRESENT
2 REAR_PRESENT
3 EXIT_PRESENT
4 ENTRY_BARRIER_OPEN_FB
5 ENTRY_BARRIER_CLOSED_FB
6 EXIT_BARRIER_OPEN_FB
7 EXIT_BARRIER_CLOSED_FB
```

`SAFETY_CLEAR` has no default address. It must be explicitly mapped. If absent it remains `false` by design, blocking automatic entry rather than assuming safety.

Default coil map:

```text
0 ENTRY_RED
1 ENTRY_GREEN
2 EXIT_RED
3 EXIT_GREEN
4 BUZZER
5 ENTRY_BARRIER_OPEN_REQUEST
6 EXIT_BARRIER_OPEN_REQUEST
```

Override addresses with `-io-map`, for example:

```text
-io-map "safety_clear=8,front_present=11,entry_red=20"
```

Supported keys:

```text
entry_present
front_present
rear_present
exit_present
entry_barrier_open_fb
entry_barrier_closed_fb
exit_barrier_open_fb
exit_barrier_closed_fb
safety_clear
entry_red
entry_green
exit_red
exit_green
buzzer
entry_barrier_open_cmd
exit_barrier_open_cmd
```

## Runtime flags

```text
-io-addr host:port
-io-unit-id 1
-io-map "safety_clear=8,..."
-io-poll 100ms
-io-timeout 1s
-io-buzzer-pulse 700ms
-barrier-feedback-timeout 5s
```

No physical I/O is written when `-io-addr` is empty.

## Operator diagnostics

```text
GET /api/io/status
```

reports:

```text
enabled
connected
last_input
desired
applied
last_success_at
last_error_at
last_error
```

`/healthz` also includes the same I/O status snapshot.

## CI acceptance

The GitHub Windows workflow uses a deterministic Modbus TCP simulator. It verifies the actual runtime path:

```text
ENTRY_PRESENT via Modbus
→ transaction starts
→ RFID/LPR identity
→ Engine requests entry barrier
→ Modbus coil changes
→ simulator returns OPEN feedback
→ reconciler permits GREEN
→ FRONT+REAR sensors
→ audited scale sequence
→ durable ticket
→ Engine requests exit barrier
→ OPEN feedback
→ GREEN
→ EXIT sensor ON/OFF
→ COMPLETE
→ no permissive applied outputs remain
```

This simulator is test-only and is never included in the production artifact.
