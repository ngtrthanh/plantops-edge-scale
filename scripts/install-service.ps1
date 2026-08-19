param(
  [string]$AppDir = "C:\WSM\PlantOps.Edge.Scale"
)

$ErrorActionPreference = "Stop"
$exe = Join-Path $AppDir "PlantOps.Edge.Scale.exe"

if (-not (Test-Path $exe)) {
  throw "Executable not found: $exe"
}

if (Get-Service "PlantOps.Edge.Scale" -ErrorAction SilentlyContinue) {
  Stop-Service "PlantOps.Edge.Scale" -ErrorAction SilentlyContinue
  sc.exe delete "PlantOps.Edge.Scale" | Out-Null
  Start-Sleep -Seconds 1
}

sc.exe create "PlantOps.Edge.Scale" binPath= "`"$exe`"" start= auto DisplayName= "PlantOps Edge Scale"
sc.exe description "PlantOps.Edge.Scale" "PlantOps Edge Scale Kestrel service"
sc.exe failure "PlantOps.Edge.Scale" reset= 86400 actions= restart/5000/restart/5000/restart/5000

Start-Service "PlantOps.Edge.Scale"
Write-Host "Installed. Open http://127.0.0.1:8080"
