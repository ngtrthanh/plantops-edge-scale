@echo off
setlocal
cd /d "%~dp0"

echo Starting PlantOps Edge Scale demo...
start "PlantOps Edge Scale" /D "%~dp0" "%~dp0PlantOps.Edge.Scale.exe"

powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$url='http://127.0.0.1:8080/healthz';" ^
  "$ok=$false;" ^
  "for($i=0;$i -lt 30;$i++){ try { $r=Invoke-RestMethod $url -TimeoutSec 1; if($r.status -eq 'ok'){ $ok=$true; break } } catch {}; Start-Sleep -Milliseconds 500 };" ^
  "if($ok){ Start-Process 'http://127.0.0.1:8080'; exit 0 } else { exit 1 }"

if errorlevel 1 (
  echo Demo failed to start. Check whether port 8080 is already in use.
  pause
  exit /b 1
)

echo Demo running at http://127.0.0.1:8080
echo Use STOP-DEMO.cmd when finished.
pause
