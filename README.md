# PlantOps Edge Scale — Kestrel + Windows Service Demo

Minimal proof-of-concept for the proposed WSM/PlantOps Edge runtime on Windows.

```text
Windows
└── PlantOps.Edge.Scale.exe
    ├── Windows Service capable
    ├── Kestrel http://127.0.0.1:8080
    ├── Backend + REST API
    ├── static FE in wwwroot
    ├── simulated scale/RFID/LPR/position inputs
    ├── simulated lights/buzzer/barrier outputs
    └── ticket write to data/tickets.jsonl
```

No IIS. No Docker Desktop. No WSL2.

## Quick local demo

GitHub Actions builds and smoke-tests a self-contained Windows x64 package. The generated ZIP contains:

```text
PlantOps.Edge.Scale.exe
RUN-DEMO.cmd
STOP-DEMO.cmd
README-LOCAL-DEMO.txt
wwwroot\...
required .NET runtime files
```

Run it on Windows:

```text
1. Extract ZIP
2. Double-click RUN-DEMO.cmd
3. Browser opens http://127.0.0.1:8080
4. Run the truck-cycle demo
5. Double-click STOP-DEMO.cmd when finished
```

No separate .NET runtime or SDK is required for the packaged demo because CI publishes it as self-contained `win-x64`.

## Demo flow

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

## APIs

```text
GET  /healthz
GET  /api/state
GET  /api/tickets
POST /api/demo/run
```

## CI verification

`.github/workflows/build-win-x64.yml` runs on a GitHub-hosted Windows runner and verifies:

```text
restore
→ build
→ self-contained win-x64 publish
→ EXE exists
→ launch Kestrel
→ /healthz
→ state API
→ simulated truck cycle
→ ticket created
→ package ZIP + SHA256
→ upload artifact
```

Artifact naming:

```text
plantops-edge-scale-local-demo-win-x64-sha-<git_sha>.zip
plantops-edge-scale-local-demo-win-x64-sha-<git_sha>.zip.sha256
```

## Run from source

```powershell
dotnet run
```

## Hardware boundary

The current demo simulates the I/O sequence only. Production adapters should replace simulator calls with real interfaces:

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
