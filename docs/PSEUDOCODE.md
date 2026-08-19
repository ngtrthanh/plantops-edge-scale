# Edge Scale — Whole-Repo Pseudocode

This is the implementation-level behavioral specification for the repository.

## 1. Process bootstrap

```text
MAIN
  load config
  load runtime build identity
  open local database
  run schema migrations

  create adapters
    scale = configured ScaleAdapter
    io = configured DigitalIOAdapter
    rfid = configured RFIDAdapter
    lpr = configured LPRAdapter
    tickets = LocalTicketRepository
    audit = LocalAuditRepository
    sync = CentralSyncAdapter

  create domain engine(adapters)
  restore unfinished transaction if policy allows

  start background workers
    hardware poll/read loops
    health monitor
    workflow engine
    sync worker
    heartbeat worker

  start embedded HTTP server
  serve embedded UI

  wait for shutdown
  stop accepting new transaction
  flush durable writes
  close adapters
END
```

## 2. Adapter contracts

```text
PORT Scale
  Read() -> ScaleReading
    weight_kg
    stable
    overload
    fault
    timestamp

PORT RFID
  Latest() -> RFIDObservation
    tag
    confidence/quality if available
    timestamp
    health

PORT LPR
  Latest() -> LPRObservation
    plate
    confidence
    image_ref
    timestamp
    health

PORT PositionIO
  ReadInputs() -> InputSnapshot
    entry_present
    front_present
    rear_present
    exit_present
    entry_barrier_open_feedback
    entry_barrier_closed_feedback
    exit_barrier_open_feedback
    exit_barrier_closed_feedback
    physical_safety_clear

PORT Outputs
  SetEntryLight(RED|GREEN)
  SetExitLight(RED|GREEN)
  PulseBuzzer(duration)
  RequestEntryBarrier(OPEN|CLOSE)
  RequestExitBarrier(OPEN|CLOSE)

PORT TicketStore
  Commit(ticket) DURABLE
  Get(id)
  ListPendingSync()

PORT AuditStore
  Append(event) DURABLE

PORT CentralSync
  PushTicket(ticket)
  PushAudit(batch)
  Heartbeat(status)
```

## 3. Hardware read normalization

```text
FOR EACH adapter observation
  validate protocol framing/checksum where applicable
  attach adapter timestamp
  normalize into domain type
  update adapter health

  IF no valid observation within stale_timeout
    health = STALE

  IF transport fails
    health = DISCONNECTED

  IF device reports internal error
    health = FAULT

  publish observation to engine
END
```

## 4. New truck transaction

```text
WHEN state == IDLE
AND entry presence becomes ON

  tx = new Transaction(UUID)
  tx.mode = NORMAL
  tx.started_at = now

  audit(TRUCK_DETECTED)
  transition(APPROACH)

  begin identity acquisition
END
```

## 5. Identity acquisition

```text
transition(IDENTIFYING)

rfid_result = wait_for_recent_RFID(timeout)
lpr_result  = wait_for_recent_LPR(timeout)

IF both healthy and present
  IF map(rfid_result.tag) matches lpr_result.plate
    identity = ACCEPTED
  ELSE
    transition(IDENTITY_MISMATCH)
    require operator resolution
  END

ELSE IF RFID failed/unavailable AND LPR healthy
  request transaction-scoped RFID override
  require operator confirmation of LPR identity

ELSE IF LPR failed/unavailable AND RFID healthy
  request transaction-scoped LPR override
  require operator/manual plate confirmation

ELSE
  mode = MANUAL
  require supervisor manual identity
END

IF identity accepted
  evaluate entry authorization
END
```

## 6. Entry authorization

```text
CAN_OPEN_ENTRY =
  identity accepted
  AND no critical lockout
  AND scale not occupied by another transaction
  AND physical safety permits motion

IF CAN_OPEN_ENTRY
  set entry light GREEN
  request entry barrier OPEN
  wait for open feedback if configured
  transition(ENTRY_AUTHORIZED)
ELSE
  entry light RED
  barrier CLOSE/hold
END
```

## 7. Entering and positioning

```text
WHEN front sensor turns ON
  transition(ENTERING)

WHEN front == ON AND rear == ON
  request entry barrier CLOSE when safe
  set entry light RED
  transition(POSITIONING)
  position_evidence = NORMAL

ELSE IF one deck sensor is FAULT
  mode = DEGRADED
  require:
    other deck sensor == ON
    AND camera/operator confirms truck on deck
  record override

ELSE IF both deck sensors unavailable
  mode = MANUAL
  disable automatic weighing progression
END
```

## 8. Ready-to-weigh rule

```text
READY_TO_WEIGH =
  identity accepted
  AND position evidence accepted
  AND scale health == HEALTHY
  AND no scale fault
  AND no overload

IF READY_TO_WEIGH
  transition(READY_TO_WEIGH)
ELSE
  remain blocked and show exact reason
END
```

## 9. Stable weight acceptance

```text
transition(WEIGHING)

LOOP
  reading = scale.Read()

  IF scale transport/device fault
    mode = FAULT_LOCKOUT
    STOP AUTO FLOW
  END

  show live weight

  IF reading.stable == TRUE
    candidate = reading.weight
    re-read/verify according to controller protocol

    IF stable still authoritative
      accepted_weight = candidate
      audit(STABLE_WEIGHT_ACCEPTED)
      BREAK
    END
  END
END
```

No software override can fabricate `stable=true` or replace the authoritative scale weight.

## 10. Ticket commit

```text
VALIDATE before commit
  tx exists
  identity accepted
  position accepted under NORMAL/DEGRADED/MANUAL policy
  authoritative stable weight exists
  no critical lockout
  all used overrides are authorized and tx-scoped

BEGIN LOCAL DB TRANSACTION
  insert ticket
  insert identity evidence
  insert position evidence
  insert scale evidence
  insert override records
  insert audit LOCAL_COMMITTED
COMMIT DB TRANSACTION DURABLY

IF commit succeeds
  transition(LOCAL_COMMITTED)
  enqueue Central sync
ELSE
  do NOT release truck
  transition(COMMIT_FAILED)
END
```

## 11. Exit authorization

```text
CAN_RELEASE = ticket.local_commit == TRUE

IF CAN_RELEASE
  pulse buzzer
  set exit light GREEN
  request exit barrier OPEN
  transition(EXIT_AUTHORIZED)
ELSE
  exit light RED
  exit barrier hold CLOSED
END
```

Central sync status is not part of `CAN_RELEASE`.

## 12. Exit detection and safe barrier close

```text
transition(EXITING) when exit presence becomes ON

NORMAL close rule:
  exit sensor returns CLEAR
  AND barrier safety input CLEAR
  AND truck no longer occupies scale

IF normal evidence healthy
  request exit barrier CLOSE

ELSE IF exit sensor failed
  mode = DEGRADED
  require camera clear OR named operator CLEAR confirmation
  then request close

IF barrier safety input indicates obstruction
  NEVER force close from app
END

wait for closed feedback if configured
set exit light RED
transition(COMPLETE)
```

## 13. Override request

```text
REQUEST_OVERRIDE(device, reason, tx)
  assert device is configured overridable
  assert tx active
  determine risk level
  gather current fallback evidence

  IF fallback evidence insufficient
    reject

  IF risk == OPERATOR
    require authenticated operator
  ELSE
    require supervisor authorization
  END

  persist override DURABLY
  attach to current transaction
  mode = DEGRADED or MANUAL according to policy
END
```

## 14. Override expiry

```text
ON COMPLETE or ABORT
  FOR every transaction override
    mark expired
  END

  do NOT clear hardware fault
  next transaction must evaluate device health again
END
```

## 15. Fault policy engine

```text
EVALUATE_MODE(snapshot)

IF scale weight/stable authority unavailable
  return FAULT_LOCKOUT

IF critical physical actuator/safety fault makes auto motion unsafe
  return FAULT_LOCKOUT or MANUAL according to configured safe procedure

count auxiliary failures by evidence class

IF zero
  return NORMAL

IF exactly one configured overridable failure
AND fallback evidence available
  return DEGRADED

IF multiple failures but supervised procedure exists
  return MANUAL

return FAULT_LOCKOUT
```

## 16. Health worker

```text
EVERY 1 second
  inspect each adapter last_seen / health
  calculate station mode
  publish live status

  IF device transitioned HEALTHY -> FAULT
    audit fault
  IF FAULT -> HEALTHY
    audit recovery
END
```

## 17. Central sync worker

```text
LOOP while running
  pending = local DB unsynced tickets/audits

  IF Central reachable
    FOR each pending item in order
      send with idempotency key
      IF ack
        mark synced
      ELSE
        backoff
        BREAK
    END
  ELSE
    report local CONTROL_OFFLINE state
  END

  sleep/backoff
END
```

## 18. HTTP API

```text
GET  /healthz
GET  /api/state
GET  /api/transaction/current
GET  /api/devices
GET  /api/events
GET  /api/tickets
GET  /api/overrides

POST /api/override/request
POST /api/override/authorize
POST /api/manual/identity
POST /api/manual/position-confirm
POST /api/manual/exit-clear
POST /api/manual/barrier-command   privileged

POST /io/rfid                     hardware ingress if webhook mode
POST /io/lpr                      hardware ingress if webhook mode
```

## 19. UI behavior

```text
UI polls or subscribes to state

always show:
  transaction phase
  system mode NORMAL/DEGRADED/MANUAL/LOCKOUT
  weight/stable state
  RFID/LPR identity
  each sensor health + value
  each actuator command + feedback
  current override banner
  exact reason workflow is blocked

never hide degraded operation
never present override as a generic bypass
```

## 20. Startup recovery

```text
ON process restart
  read last transaction

IF no active tx
  start IDLE

ELSE
  do not blindly replay actuator commands
  read fresh physical inputs
  reconstruct safe state
  require operator confirmation if physical state is ambiguous
  audit PROCESS_RECOVERY
END
```

## 21. Deployment

```text
CI
  go test ./...
  go vet ./...
  build windows/amd64 single exe
  inject version + git SHA
  launch binary on Windows runner
  test /healthz
  run simulated full truck cycle
  verify ticket + override tests
  package exe + config example + service scripts
  SHA256
  upload immutable artifact
```
