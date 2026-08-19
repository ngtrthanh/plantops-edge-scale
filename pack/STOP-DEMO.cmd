@echo off
taskkill /IM PlantOps.Edge.Scale.exe /F >nul 2>&1
if errorlevel 1 (
  echo PlantOps Edge Scale demo was not running.
) else (
  echo PlantOps Edge Scale demo stopped.
)
pause
