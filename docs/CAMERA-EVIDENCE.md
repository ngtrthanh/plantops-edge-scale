# Camera evidence contract

## Installed topology

Default runtime configuration reflects the current three-camera site topology:

```text
C1A  A-side LPR / A -> B identity evidence
C1B  B-side LPR / B -> A identity evidence
C3   weighbridge / vehicle / load overview
```

Optional cameras such as C2/C4 may be added through configuration without changing the workflow domain.

```text
-camera-ids C1A,C1B,C3
```

## Ingress

Directional LPR:

```text
POST /io/lpr
{
  "plate": "15C-123.45",
  "confidence": 98.5,
  "image_ref": "evidence/...jpg",
  "camera_id": "C1A"
}
```

`camera_id` may be omitted for installed C1 evidence. The active physical direction maps automatically:

```text
A_TO_B -> C1A
B_TO_A -> C1B
```

Overview / additional camera evidence:

```text
POST /io/camera/C3
{
  "role": "OVERVIEW",
  "image_ref": "evidence/...jpg",
  "captured_at": "..."
}
```

Only configured camera IDs are accepted.

## Persistence

Evidence is attached to the active short physical pass before its stable weight is committed.

```text
CameraEvidence
-> Transaction.CameraEvidence
-> WeighPass.CameraEvidence
-> WeighCycle first/second pass
-> final Ticket.CameraEvidence (both passes aggregated)
```

SQLite stores pass/ticket JSON, so evidence references remain part of the durable business record without adding vendor-specific image storage to the Edge core.

## Audit

Every accepted evidence link is recorded synchronously as `CAMERA_EVIDENCE` in the hash-chained operational journal.

The Edge stores references, timestamps, camera IDs and roles. Actual image bytes may remain on camera/NVR/media storage. Their retention and cryptographic object integrity are deployment policies outside the scale controller process.

## Safety

Camera evidence is auxiliary. It may support identity/degraded procedures, but it never overrides authoritative scale weight/stable/fault or physical safety interlocks.
