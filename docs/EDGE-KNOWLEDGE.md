# Edge Scale — Operating Knowledge

This document is the current design truth for the WSM/PlantOps Edge Scale runtime.

## 1. Core intent

Edge Scale is an offline-first local controller for an unmanned truck weighbridge.

It owns the transaction workflow around the scale, identity, position confirmation, traffic signaling, barrier authorization, local ticket commit, audit trail, and later synchronization to Central.

Central availability must never be required to complete a valid local weighing transaction.

## 2. Runtime target

vNext target:

```text
Windows
└── plantops-edge-scale.exe        single Go binary
    ├── embedded web UI
    ├── REST API
    ├── workflow/state machine
    ├── hardware adapters
    ├── local persistence
    ├── audit/events
    └── sync worker
```

No IIS. No Docker Desktop. No WSL2. No Node runtime. No separate application server.

The C# implementation is retained only as a validated executable specification during migration.

## 3. Physical inputs

```text
Scale controller
├── weight
├── stable signal
├── fault/overload status if available
└── connection health

Identity
├── RFID reader
└── plate camera / LPR result

Position / presence
├── entry sensor
├── front deck sensor
├── rear deck sensor
└── exit sensor

Actuator feedback where available
├── entry barrier open/closed
└── exit barrier open/closed
```

## 4. Physical outputs

```text
entry red/green light
exit red/green light
buzzer
entry barrier command
exit barrier command
```

Application commands are business-workflow commands. Equipment safety must retain an independent physical safety layer where required.

## 5. Normal truck cycle

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
→ IDLE
```

Detailed sequence:

```text
truck arrives
→ entry presence detected
→ read RFID
→ capture/process plate
→ cross-check identity
→ entry green
→ entry barrier open
→ truck enters
→ front/rear sensors confirm deck position
→ entry barrier close
→ wait for stable scale
→ validate transaction/interlocks
→ commit ticket LOCALLY
→ buzzer pulse
→ exit green
→ exit barrier open
→ truck leaves
→ exit-clear evidence
→ exit barrier close
→ complete
```

`LOCAL_COMMITTED`, not Central synchronization, authorizes release.

## 6. Operating modes

```text
NORMAL
DEGRADED
MANUAL
FAULT_LOCKOUT
```

### NORMAL

All required devices and evidence are healthy.

### DEGRADED

Exactly one or an explicitly allowed auxiliary device is failed/unavailable, but configured fallback evidence is sufficient.

### MANUAL

Automation evidence is insufficient but the situation is operationally recoverable under named operator/supervisor control.

### FAULT_LOCKOUT

A non-overridable or unsafe combination has failed. Automatic transaction progression stops.

## 7. Override policy

### Non-overridable

The scale controller is never software-overridden for:

```text
weight value
stable signal
scale fault/overload
```

If authoritative weight/stability is unavailable:

```text
NO AUTO WEIGH
NO TICKET COMMIT
NO AUTO RELEASE
→ manual maintenance / certified weighing procedure outside normal auto flow
```

### Transaction-scoped override only

Never implement permanent flags like:

```text
IgnoreFrontSensor = true
```

Use:

```text
Override {
  transaction_id
  device
  reason
  requested_by
  authorized_by
  authorized_at
  evidence[]
  expires_at_transaction_end = true
}
```

At `COMPLETE`, every transaction override expires automatically. The failed device remains FAULT for the next transaction.

### Fallback examples

```text
RFID failed
→ LPR + operator identity confirmation

LPR/OCR failed
→ RFID + camera image/manual plate confirmation

front position sensor failed
→ rear sensor + camera/manual position confirmation + stable scale

rear position sensor failed
→ front sensor + camera/manual position confirmation + stable scale

exit sensor failed
→ camera clear OR operator CLEAR confirmation before barrier close
```

If both primary position sensors fail, automatic weighing is disabled and the transaction moves to MANUAL.

If both RFID and LPR are unavailable, manual identity requires supervisor authorization.

Loss of a critical actuator or unsafe barrier feedback disables automatic mode.

## 8. Evidence model

Do not make one auxiliary sensor the truth of the whole workflow.

Use independent evidence classes:

```text
IDENTITY
POSITION
WEIGHT
EXIT_CLEAR
ACTUATOR_FEEDBACK
```

Normal position evidence:

```text
front_sensor == ON
AND rear_sensor == ON
```

Degraded position evidence example:

```text
one healthy deck sensor
AND camera/operator position confirmation
AND authoritative stable scale
```

Weight is evidence for weighing, not a substitute for vehicle position detection.

## 9. Override authorization levels

### Operator-level, one transaction

Allowed for configured single auxiliary failures where fallback evidence is strong.

### Supervisor-level

Required for higher-risk degraded cases such as:

```text
both identity channels unavailable
multiple correlated sensors failed
barrier feedback unavailable
fully manual identity
```

There is no `IGNORE ALL SENSORS` operation.

## 10. Barrier rules

Barrier motion is physically sensitive.

### Exit opening

Normal automatic rule:

```text
local_ticket_committed
AND scale_acceptance_complete
AND identity_accepted
AND position/evidence policy satisfied
→ EXIT_AUTHORIZED
→ green + buzzer + barrier OPEN
```

WAN/Central outage must not trap a truck after a valid local commit.

### Exit closing

Never blindly close only because a timer expired.

Preferred close evidence:

```text
exit sensor clear
AND barrier safety input clear
```

If exit sensor is faulted:

```text
camera clear
OR named operator CLEAR confirmation
```

Physical photocell/loop/laser safety supplied by the barrier system remains authoritative for collision prevention.

## 11. Fault semantics

Each hardware adapter reports:

```text
HEALTHY
STALE
DISCONNECTED
FAULT
```

plus last successful timestamp and diagnostic text.

The domain engine decides operational mode. Adapters do not decide business overrides.

## 12. Audit requirements

Audit all:

```text
state transitions
raw/normalized hardware observations
identity match/mismatch
stable weight acceptance
ticket commits
output commands
barrier feedback
faults
operator actions
override requests/authorization/use/expiry
manual clear
manual release
```

Minimum audit fields:

```text
station_id
transaction_id
timestamp_utc
actor
source/device
action
old_state
new_state
reason
evidence
result
runtime_git_sha
```

## 13. Local-first persistence

Recommended Go target:

```text
single executable
+ one local SQLite database file
+ optional image directory/cache
```

SQLite is preferred over a local PostgreSQL service for this small Edge controller unless a future requirement proves otherwise.

Ticket commit and override audit must be durable before release is authorized.

## 14. Central synchronization

```text
local durable commit
→ enqueue sync
→ attempt Central delivery
→ acknowledgement
→ mark synced
```

Central outage:

```text
weighing continues
local DB continues
pending queue grows
heartbeat reports CONTROL_OFFLINE
catch-up sync after recovery
```

Central outage alone never restarts, upgrades, downgrades, blocks, or releases a vehicle.

## 15. Software architecture rule

Domain logic depends only on ports/interfaces.

```text
Domain Engine
    ↓ ports
Adapters
├── scale TCP/serial
├── Modbus TCP digital I/O
├── RFID Ethernet/webhook
├── LPR camera Ethernet/webhook
├── local repository
└── Central sync
```

Vendor protocol code must never leak into the workflow/state machine.

## 16. Deployment identity

Every release is immutable and identified by exact Git SHA.

```text
build once
→ artifact sha-<git_sha>
→ deploy exact artifact
→ health check
→ verify running_sha == desired_sha
→ deployment receipt
→ heartbeat
→ rollback if verification fails
```

No production `latest` identity.
