$ErrorActionPreference = 'Stop'
$weightPath = Join-Path $PWD 'out/raw-weight-ci.jsonl'
$eventPath = Join-Path $PWD 'out/events-ci.jsonl'
$dbPath = Join-Path $PWD 'out/edge-ci.db'
$logPath = Join-Path $PWD 'out/runtime-ci.log'
Remove-Item $weightPath,$eventPath,$dbPath,"$dbPath-wal","$dbPath-shm",$logPath -ErrorAction SilentlyContinue

$scaleSim = Start-Process .\out\scale-sim.exe -PassThru
Start-Sleep -Seconds 1
$p = Start-Process .\out\plantops-edge-scale.exe -ArgumentList @(
  '-scale-addr','127.0.0.1:19001','-station-id','CI-SCALE-01',
  '-raw-weight-journal',$weightPath,'-event-journal',$eventPath,'-db',$dbPath,'-log-file',$logPath,
  '-vehicle-map','RFID-DEMO-001=15C-123.45','-empty-scale-max-kg','200',
  '-min-stable-weight-kg','1000','-stable-confirmations','2','-stable-tolerance-kg','20','-simulation'
) -PassThru
try {
  $ok=$false
  for($i=0;$i -lt 50;$i++){
    try{
      $h=Invoke-RestMethod http://127.0.0.1:8080/healthz -TimeoutSec 1
      $scale=Invoke-RestMethod http://127.0.0.1:8080/api/scale/status -TimeoutSec 1
      $storage=Invoke-RestMethod http://127.0.0.1:8080/api/storage/status -TimeoutSec 1
      if($h.status -eq 'ok' -and $storage.integrity -eq 'ok' -and $scale.connected -and $scale.last_reading.stable -and [int64]$scale.last_reading.weight_kg -eq 0){$ok=$true;break}
    }catch{}
    Start-Sleep -Milliseconds 200
  }
  if(-not $ok){throw 'startup/SQLite/stable-zero proof failed'}

  $entry=Invoke-RestMethod http://127.0.0.1:8080/sim/position -Method Post -ContentType 'application/json' -Body '{"entry_present":true,"safety_clear":true}'
  if($entry.state -ne 'IDENTIFYING'){throw "truck state=$($entry.state)"}
  Invoke-RestMethod http://127.0.0.1:8080/io/rfid -Method Post -ContentType 'application/json' -Body '{"tag":"RFID-DEMO-001","quality":99}' | Out-Null
  Invoke-RestMethod http://127.0.0.1:8080/io/lpr -Method Post -ContentType 'application/json' -Body '{"plate":"15C-123.45","confidence":98}' | Out-Null
  $wf=Invoke-RestMethod http://127.0.0.1:8080/api/workflow
  if($wf.state -ne 'ENTRY_AUTHORIZED'){throw 'identity/entry authorization failed'}
  $positioned=Invoke-RestMethod http://127.0.0.1:8080/sim/position -Method Post -ContentType 'application/json' -Body '{"front_present":true,"rear_present":true,"safety_clear":true}'
  if($positioned.transaction.position -ne 'ACCEPTED'){throw 'position not accepted'}

  $committed=$false
  for($i=0;$i -lt 60;$i++){
    $wf=Invoke-RestMethod http://127.0.0.1:8080/api/workflow
    if($wf.state -eq 'EXIT_AUTHORIZED'){$committed=$true;break}
    if($wf.state -eq 'FAULT_LOCKOUT'){throw "lockout: $($wf.transaction.last_block_reason)"}
    Start-Sleep -Milliseconds 200
  }
  if(-not $committed){throw 'stable weight did not commit'}
  if([int64]$wf.transaction.accepted_weight.weight_kg -ne 28460){throw 'accepted weight mismatch'}

  $storage=Invoke-RestMethod http://127.0.0.1:8080/api/storage/status
  if([int64]$storage.tickets -ne 1 -or [int64]$storage.pending_sync -ne 1 -or $storage.integrity -ne 'ok'){throw "SQLite atomic state invalid: $($storage|ConvertTo-Json -Compress)"}
  $weights=Invoke-RestMethod 'http://127.0.0.1:8080/api/audit/weights?limit=50'
  $acceptedSeq=[uint64]$wf.transaction.accepted_weight.raw_ref.seq
  $acceptedHash=[string]$wf.transaction.accepted_weight.raw_ref.hash
  $rawAccepted=$weights.records|Where-Object{[uint64]$_.seq -eq $acceptedSeq}|Select-Object -First 1
  if($null -eq $rawAccepted -or $rawAccepted.hash -ne $acceptedHash){throw 'ticket -> raw audit linkage failed'}

  $null=Invoke-RestMethod http://127.0.0.1:8080/sim/position -Method Post -ContentType 'application/json' -Body '{"exit_present":true,"safety_clear":true}'
  $complete=Invoke-RestMethod http://127.0.0.1:8080/sim/position -Method Post -ContentType 'application/json' -Body '{"exit_present":false,"safety_clear":true}'
  if($complete.state -ne 'COMPLETE'){throw 'workflow did not complete'}
  $events=Invoke-RestMethod 'http://127.0.0.1:8080/api/audit/events?limit=250'
  $kinds=@($events.records|ForEach-Object{$_.event.kind})
  foreach($required in @('TRANSACTION_STARTED','RFID_OBSERVED','LPR_OBSERVED','IDENTITY_DECISION','POSITION_DECISION','STABLE_WEIGHT_ACCEPTED','TICKET_COMMITTED','TRANSACTION_COMPLETED')){if($kinds -notcontains $required){throw "event audit missing $required"}}
  if(-not (Invoke-RestMethod http://127.0.0.1:8080/api/audit/events/verify).verified){throw 'event hash chain failed'}
} finally {
  if($p -and -not $p.HasExited){Stop-Process -Id $p.Id -Force}
  if($scaleSim -and -not $scaleSim.HasExited){Stop-Process -Id $scaleSim.Id -Force}
}

$p2=Start-Process .\out\plantops-edge-scale.exe -ArgumentList @('-listen','127.0.0.1:8081','-station-id','CI-SCALE-01','-db',$dbPath,'-raw-weight-journal',$weightPath,'-event-journal',$eventPath,'-log-file',$logPath) -PassThru
try{
  $ready=$false
  for($i=0;$i -lt 40;$i++){
    try{
      $h2=Invoke-RestMethod http://127.0.0.1:8081/healthz -TimeoutSec 1
      $s2=Invoke-RestMethod http://127.0.0.1:8081/api/storage/status -TimeoutSec 1
      $wf2=Invoke-RestMethod http://127.0.0.1:8081/api/workflow -TimeoutSec 1
      $io2=Invoke-RestMethod http://127.0.0.1:8081/api/io/status -TimeoutSec 1
      if($h2.status -eq 'ok' -and $s2.integrity -eq 'ok' -and [int64]$s2.tickets -eq 1 -and [int64]$s2.pending_sync -eq 1 -and $wf2.state -eq 'IDLE' -and -not $io2.enabled){$ready=$true;break}
    }catch{}
    Start-Sleep -Milliseconds 200
  }
  if(-not $ready){throw 'restart recovery failed'}
  Write-Host 'Runtime PASS: audit + SQLite atomic commit + restart recovery'
}finally{if($p2 -and -not $p2.HasExited){Stop-Process -Id $p2.Id -Force}}
