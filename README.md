# PlantOps Edge Scale — Kestrel + Windows Service Demo

Minimal proof-of-concept for the proposed WSM/PlantOps Edge runtime on Windows.

```text
Windows
└── PlantOps.Edge.Scale.exe
    ├── Windows Service
    ├── Kestrel http://127.0.0.1:8080
    ├── Backend + REST API
    ├── static FE in wwwroot
    ├── simulated scale/RFID/LPR/position inputs
    ├── simulated lights/buzzer/barrier outputs
    └── ticket write to data/tickets.jsonl
```

No IIS. No Docker Desktop. No WSL2.

## Demo flow

Open `http://127.0.0.1:8080` and press **Run truck cycle**.

```text
truck detected
→ RFID + plate identified
→ entry light GREEN
→ entry barrier OPEN
→ truck enters
→ position sensors ON
→ entry barrier CLOSED
→ weight becomes STABLE
→ ticket saved
→ buzzer
→ exit light GREEN
→ exit barrier OPEN
→ truck leaves
→ exit barrier CLOSED
```

## Run from source

```powershell
dotnet run
```

## APIs

```text
GET  /healthz
GET  /api/state
GET  /api/tickets
POST /api/demo/run
```

## GitHub Actions build

The workflow `.github/workflows/build-win-x64.yml` runs on a GitHub-hosted Windows runner, installs .NET SDK 10.0.302, compiles the project, publishes a self-contained `win-x64` build, and uploads:

```text
plantops-edge-scale-win-x64-sha-<git_sha>.zip
plantops-edge-scale-win-x64-sha-<git_sha>.zip.sha256
```

The target Edge PC therefore does not need the .NET SDK or a separate .NET runtime for this demo artifact.

## Install as Windows Service

Extract the published folder to:

```text
C:\WSM\PlantOps.Edge.Scale\
```

Then run elevated PowerShell:

```powershell
.\scripts\install-service.ps1
```

Check:

```powershell
Get-Service PlantOps.Edge.Scale
curl http://127.0.0.1:8080/healthz
```

## Hardware boundary

The current demo simulates the I/O sequence only. Production adapters should replace the simulator calls with real interfaces:

```text
Scale        → TCP/serial
RFID         → TCP/serial/HTTP
LPR/plate    → HTTP/event callback
Position     → PLC/digital input
Lights       → PLC/relay output
Buzzer       → PLC/relay output
Barriers     → PLC/relay output
Ticket       → local PostgreSQL
```

The physical safety PLC/interlock remains authoritative for safe equipment movement. The application should request outputs; it should not be the only safety layer.
