# Edge Scale — Hardware & Wiring Plan

Status: architecture proposal / procurement shortlist, not a purchase order.

The key design goal is to minimize vendor-specific wiring and protocol surface. Keep the scale controller authoritative, move auxiliary dry-contact/24 V I/O to Ethernet remote I/O, and keep RFID/LPR on Ethernet.

## 1. Recommended topology

```text
                         ┌──────────────────────────────┐
                         │ Edge PC / Windows            │
                         │ plantops-edge-scale.exe      │
                         └──────────────┬───────────────┘
                                        │ Ethernet
                               ┌────────▼────────┐
                               │ Industrial PoE  │
                               │ Ethernet switch │
                               └───┬────┬────┬───┘
                                   │    │    │
                    ┌──────────────┘    │    └──────────────┐
                    │                   │                   │
             ┌──────▼──────┐     ┌──────▼──────┐     ┌──────▼──────┐
             │ Remote I/O  │     │ RFID reader │     │ LPR camera  │
             │ Modbus TCP  │     │ Ethernet    │     │ PoE/Ethernet│
             └──────┬──────┘     └─────────────┘     └─────────────┘
                    │
             24 V DI/DO / interposing relays
                    │
        ┌───────────┼──────────────────────────────────────┐
        │           │             │             │          │
     sensors     lights         buzzer       barriers   feedback

Scale controller
  ├── native Ethernet/TCP preferred
  └── if serial-only: industrial serial-device-server → Ethernet
```

## 2. Hardware classes

### 2.1 Existing scale controller — KEEP

Do not replace or weaken the certified/authoritative scale controller merely to simplify software.

Preferred integration order:

```text
1. native Ethernet/TCP protocol from controller
2. RS-485/RS-232 through industrial serial-to-Ethernet gateway
3. direct serial only if the PC/cabling environment is controlled
```

The Go `scale` adapter owns transport + vendor framing/parser. The domain only receives:

```text
weight_kg
stable
fault/overload if available
timestamp
health
```

`stable` and `weight` are non-overridable.

A current industrial reference for serial-to-Ethernet is the Moxa NPort 5000 family (for example P5150A where PoE is useful). Exact model must match the controller electrical interface (RS-232/422/485).

### 2.2 Remote digital I/O — recommended center of auxiliary wiring

Reference family: **Moxa ioLogik E1200**.

Why it fits this architecture:

- Ethernet remote I/O;
- Modbus TCP support;
- REST API also available;
- models with mixed DI/DIO;
- wide-temperature `-T` variants;
- 24 V industrial wiring.

Reference starting point: **ioLogik E1212 / E1212-T** (8 DI + 8 DIO). If the final signal count exceeds capacity, add a second I/O module instead of overloading one channel design.

Use interposing relays where output current/voltage, electrical isolation, barrier controller requirements, or maintenance practice make direct transistor output inappropriate.

### 2.3 RFID

Industrial reference: **Impinj R700 RAIN RFID reader**.

Useful properties for this design:

- fixed Ethernet reader;
- current firmware supports HTTPS/REST-oriented management and reader event webhooks;
- robust read-zone control and external antenna options;
- can remain a standalone network appliance while the Go app consumes normalized tag observations.

This is a high-end reference. If the site already has a reliable RFID reader, keep it and implement its adapter instead of buying R700 solely for software uniformity.

Preferred integration:

```text
reader event/webhook → POST /io/rfid → Go RFID ingress adapter
```

Alternative:

```text
Go adapter polls/subscribes using vendor protocol
```

### 2.4 Plate/LPR camera

Reference for a slow-speed weighbridge: **AXIS P3285-LVE Kit License Plate Verifier**.

It is purpose-tuned for slow-speed license-plate recognition and runs plate analytics on the camera, so the Edge PC does not need to run a heavy vision model merely to extract the plate.

Preferred integration:

```text
camera LPR event → POST /io/lpr
  plate
  confidence
  timestamp
  image/event reference
```

Keep a separate overview camera if operational evidence needs a full-truck view; do not force one LPR camera to do both jobs poorly.

### 2.5 Position / presence sensors

Use industrial 24 V sensors with clean digital outputs suitable for the selected remote I/O.

Reference class: compact industrial photoelectric sensor such as Banner Q20-2 series where optical geometry is suitable.

For each installation point choose sensing technology based on environment:

```text
photoelectric / retroreflective
inductive loop / magnetic vehicle detector
laser presence detector
barrier-native photocell/safety detector
```

Do not standardize the physical sensing technology before checking dust, rain, sunlight, mounting distance, truck geometry, and maintenance access.

### 2.6 Barrier

Use an industrial vehicle barrier with its own controller, safety inputs, obstruction handling, and manual release.

Reference options:

- **CAME GARD GT8** for wide industrial entrances; current product information includes Modbus integration support and industrial duty features.
- **FAAC B680H** for intensive use; integrated encoder and continuous-use positioning are useful references.

The Go app should request `OPEN`/`CLOSE`; it must not replace the barrier controller's own motion/safety electronics.

### 2.7 Physical safety relay

Reference: **Siemens SIRIUS 3SK** family.

Use a dedicated physical safety circuit where the risk assessment requires emergency-stop, protective sensor, or fail-safe intervention. The application can read safety status but must not be the only safety layer.

### 2.8 Industrial PoE switch

Reference: **Phoenix Contact FL SWITCH 1000/1100 PoE** family.

A DIN-rail PoE switch is a clean way to connect/power LPR/overview cameras while retaining industrial power and cabinet wiring practice.

Select actual port count and PoE budget after camera/RFID topology is final.

## 3. Proposed signal list

Start with this minimum logical map.

### Digital inputs

```text
DI0  ENTRY_PRESENT
DI1  FRONT_DECK_PRESENT
DI2  REAR_DECK_PRESENT
DI3  EXIT_PRESENT
DI4  ENTRY_BARRIER_OPEN_FB
DI5  ENTRY_BARRIER_CLOSED_FB
DI6  EXIT_BARRIER_OPEN_FB
DI7  EXIT_BARRIER_CLOSED_FB
```

If safety-clear, loop detector, manual switch, or extra feedback is required, add a second module rather than hiding signals in undocumented wiring.

### Digital outputs

```text
DO0  ENTRY_RED
DO1  ENTRY_GREEN
DO2  EXIT_RED
DO3  EXIT_GREEN
DO4  BUZZER
DO5  ENTRY_BARRIER_OPEN_REQUEST
DO6  EXIT_BARRIER_OPEN_REQUEST
DO7  SPARE / barrier-close request if controller needs explicit close
```

Prefer barrier controllers that can safely interpret maintained/pulse open requests and manage closing via their own configured safety logic. If explicit open/close pairs are required, allocate channels accordingly.

## 4. Safety circuit separation

```text
SOFTWARE CONTROL PATH
Go app → Modbus I/O → interposing relay → barrier controller command input

PHYSICAL SAFETY PATH
photocell / loop / safety detector / E-stop
    → barrier controller / safety relay
    → inhibit/reverse/stop motion independent of Go app
```

The software can observe physical safety status, but a software crash must not defeat anti-collision protection.

## 5. Adapter map

```text
Hardware                         Go adapter
────────────────────────────────────────────────────
Scale controller TCP            adapters/scaleascii or vendor scale adapter
Serial scale controller         NPort + same TCP-facing scale adapter
Moxa ioLogik E1200              adapters/modbustcp
Impinj / existing RFID          adapters/rfidwebhook or vendor reader
Axis / existing LPR             adapters/lprwebhook or VAPIX-specific adapter
Ticket persistence              adapters/localstore
Central API                     adapters/centralsync
```

## 6. Wire commissioning sequence

Do not wire everything and then debug the whole plant at once.

```text
Phase 1 — read-only
  connect remote I/O
  verify every DI individually
  label terminal + logical channel
  no barrier motion from app

Phase 2 — harmless outputs
  verify red/green lights
  verify buzzer

Phase 3 — barrier request dry-run
  disconnect actuator or use barrier test input
  verify correct command polarity and pulse duration

Phase 4 — supervised barrier motion
  one barrier at a time
  validate open feedback
  validate closed feedback
  validate physical safety interruption
  validate manual release

Phase 5 — identity
  RFID event → correct truck
  LPR event → correct plate
  mismatch path

Phase 6 — scale read-only
  compare Go value with controller display
  verify stable transition
  verify disconnect/fault

Phase 7 — full supervised cycle

Phase 8 — degraded/override fault injection
  unplug RFID
  block LPR
  disconnect one position sensor
  disconnect exit sensor
  disconnect remote I/O
  disconnect Central/WAN
  verify expected mode and no unsafe automatic action
```

## 7. Required fault-injection acceptance tests

```text
scale disconnected              → FAULT_LOCKOUT
scale stable missing             → no commit/release
RFID failed                      → DEGRADED path available
LPR failed                       → DEGRADED path available
front sensor failed              → fallback evidence required
rear sensor failed               → fallback evidence required
front + rear failed              → MANUAL, no auto weigh
exit sensor failed               → no blind auto close
remote I/O disconnected          → safe outputs / lockout according to hardware state
Central disconnected             → local weighing continues
app process killed               → physical barrier safety still works
Windows reboot                   → no blind replay of old barrier command
```

## 8. Procurement principle

Buy hardware by interface and safety requirement, not by brand loyalty.

Mandatory characteristics before a product is accepted:

```text
industrial temperature/environment suitable for site
24 V industrial I/O compatibility where applicable
published protocol/API
local manual operation
clear fault/status feedback
replacement availability
vendor documentation
surge/isolation plan
maintainable terminal labeling
```
