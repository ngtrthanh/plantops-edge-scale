# PlantOps Edge Scale — Kestrel + Windows Service Demo

Minimal proof-of-concept for the proposed WSM/PlantOps Edge runtime on Windows.

```text
Windows
└── PlantOps.Edge.Scale.exe
    ├── Windows Service capable
    ├── Kestrel http://127.0.0.1:8080
    ├── Backend + REST API
    ├── animated static FE in wwwroot
    ├── simulated scale/RFID/LPR/position inputs
    ├── simulated lights/buzzer/barrier outputs
    ├── live event stream
    └── ticket write to data/tickets.jsonl
```

No IIS. No Docker Desktop. No WSL2.

## Animated hardware demo

The browser demo visualizes one realistic truck cycle across the weighbridge:

```text
truck arrives
→ entry sensor detects truck
→ RFID reader scans and matches tag
→ plate camera captures image
→ OCR processes and identifies plate
→ entry light GREEN
→ entry barrier OPEN
→ truck drives onto scale
→ front + rear position sensors confirm correct position
→ entry barrier CLOSE
→ weight settles
→ stable weight accepted
→ local ticket written
→ buzzer sounds
→ exit light GREEN
→ exit barrier OPEN
→ truck leaves
→ exit sensor clears
→ exit barrier CLOSE
→ safe idle state
```

The UI includes:

- animated truck movement;
- live weighbridge weight;
- RFID reader status;
- plate camera capture + OCR status;
- entry/front/rear/exit position sensors;
- entry/exit traffic lights;
- entry/exit barriers;
- buzzer indication;
- workflow timeline;
- live Edge event stream;
- saved ticket list.

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
4. Click Run truck cycle
5. Double-click STOP-DEMO.cmd when finished
```

No separate .NET runtime or SDK is required for the packaged demo because CI publishes it as self-contained `win-x64`.

## APIs

```text
GET  /healthz
GET  /api/state
GET  /api/events
GET  /api/tickets
POST /api/demo/run
POST /api/reset
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

## Production hardware boundary

The current demo simulates the I/O sequence. Production adapters should replace simulator calls with real interfaces:

```text
Scale        → TCP/serial
RFID         → TCP/serial/HTTP
LPR/plate    → camera HTTP/event callback + OCR/LPR result
Position     → PLC/digital inputs
Lights       → PLC/relay outputs
Buzzer       → PLC/relay output
Barriers     → PLC/relay outputs
Ticket       → local PostgreSQL
```

The physical safety PLC/interlock remains authoritative for safe equipment movement. The Edge application coordinates business workflow and requests outputs; it must not be the only equipment safety layer.
