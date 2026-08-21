# Animated Business-Cycle Operator UI

The operator frontend is a real embedded HTML/CSS/JavaScript UI. It does not use a dashboard screenshot, background photo, video, Canvas, React runtime, or external CDN.

## Visual business story

```text
LOADED INBOUND
A -> B
  -> PASS 1 GROSS
  -> QUEUED / UNLOAD AT WAREHOUSE
  -> EMPTY RETURN B -> A
  -> PASS 2 TARE
  -> NET COMPLETE
```

The central digital-twin scene uses an inline SVG dump truck. Separate SVG/CSS groups represent:

- truck cab/chassis/wheels
- dump body
- material load
- hydraulic cylinder
- number plate

This lets the browser animate truck translation, wheel rotation, body tipping, cargo disappearance, and unloading particles without raster imagery.

The scene also includes native HTML/CSS/SVG representations of the weigh deck, warehouse/stockpile, barriers, sensors, RFID readers, camera coverage and outbound/return paths.

## Live mode

Live mode renders backend truth from:

- `/api/workflow`
- `/api/scale/status`
- `/api/io/status`
- `/api/identity`
- `/api/storage/status`
- `/api/queue`
- `/api/tickets/latest`
- `/api/central/status`
- raw/event audit APIs

Browser state never authorizes hardware.

## Visual Demo

`/operator.html?demo=1` runs a client-only animation sequence:

1. loaded truck enters from Side A
2. gross weighing
3. first pass becomes queued
4. truck moves to warehouse and tips the dump body
5. cargo disappears / empty truck returns from Side B
6. tare weighing
7. net calculation and complete

It performs no hardware writes and no database/ticket commit.

`/operator.html?demo=WEIGHING` intentionally holds a deterministic PASS 1 / WEIGHING frame at 28,420 kg for Chromium CI.
