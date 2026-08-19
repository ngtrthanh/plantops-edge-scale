PlantOps Edge Scale - Local Demo
================================

Architecture:
- Windows native .NET
- ASP.NET Core + Kestrel on http://127.0.0.1:8080
- No IIS
- No Docker Desktop
- No WSL2
- Self-contained win-x64 build: no separate .NET runtime required

Run:
1. Extract the ZIP to a writable folder.
2. Double-click RUN-DEMO.cmd.
3. Browser opens http://127.0.0.1:8080.
4. Run the simulated truck cycle.
5. Double-click STOP-DEMO.cmd when finished.

Demo I/O:
- scale + stable weight
- RFID
- plate/LPR
- entry/front/rear/exit position sensors
- entry/exit lights
- buzzer
- entry/exit barriers
- ticket write to data\tickets.jsonl

This package simulates hardware I/O only. It must not be connected directly to real safety-critical barrier/PLC outputs without the production hardware adapter and safety interlock design.
