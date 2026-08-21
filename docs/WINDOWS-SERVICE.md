# Native Windows Service runtime

The production Edge remains one Go executable. No IIS, Docker, WSL, .NET runtime, Node runtime, or separate service wrapper is required.

## Service commands

Run from an elevated terminal:

```text
plantops-edge-scale.exe -service install [normal runtime flags...]
plantops-edge-scale.exe -service start
plantops-edge-scale.exe -service status
plantops-edge-scale.exe -service stop
plantops-edge-scale.exe -service uninstall
```

Installation preserves the normal runtime flags as Windows Service arguments while removing only the service-management action.

## SCM configuration

```text
Service name: PlantOpsEdgeScale
Start: Automatic + Delayed Auto Start
Recovery #1: restart after 5s
Recovery #2: restart after 15s
Recovery #3: restart after 60s
Recovery reset: 24h
```

## Shutdown contract

SCM Stop or Windows Shutdown:

```text
cancel application context
-> stop collectors / I/O loops / Central worker
-> force physical SafeState using a NEW non-cancelled shutdown context
   GREEN OFF
   buzzer OFF
   side-A OPEN request OFF
   side-B OPEN request OFF
-> HTTP graceful shutdown
-> SQLite WAL checkpoint
-> service STOPPED
```

A cancelled runtime context is deliberately not reused for SafeState because cancellation must never suppress a de-permissive command.

## Restart contract

On service restart:

- audit chains are verified before runtime begins
- SQLite `integrity_check` must pass
- physical state machine starts IDLE
- queued/called business cycles remain durable in SQLite
- no stale permissive output is replayed
- remote I/O establishes SafeState before workflow commands are honored

## CI acceptance

`verify-windows-service.yml` installs the built EXE into actual Windows SCM on the hosted Windows runner, verifies RUNNING + `/healthz`, stops it, verifies the database, restarts against the same `edge.db`, verifies integrity, stops and uninstalls.
