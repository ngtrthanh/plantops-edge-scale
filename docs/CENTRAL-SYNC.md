# Central sync and heartbeat

PlantOps Edge Scale is offline-first. Central connectivity is never part of local weighing, ticket completion, or truck release authorization.

## Local durability first

A valid second pass completes locally in SQLite and enqueues the final paired ticket in `sync_queue` in the same transaction.

```text
valid A->B + B->A pair
-> SQLite COMPLETE + final ticket + sync_queue
-> local release may proceed
-> Central delivery happens asynchronously
```

If Central is offline, the queue remains durable and weighing continues.

## Generic HTTP adapter

The single binary accepts optional endpoint configuration:

```text
-central-ticket-url
-central-heartbeat-url
-central-token
-central-sync-interval
-central-heartbeat-interval
-central-timeout
```

No vendor/server contract is hard-coded beyond JSON POST and HTTP 2xx acknowledgement. A deployment-specific Central service may implement these endpoints without changing the Edge domain model.

## Ticket delivery

The worker reads durable pending `TICKET` items in order. For each item:

```text
append CENTRAL_SYNC_ATTEMPT audit
POST exact queued ticket JSON
2xx
  -> atomically AckSync
  -> mark normalized ticket synced_at
  -> append CENTRAL_SYNC_ACK audit
non-2xx / network error
  -> keep pending
  -> increment attempt count
  -> record last error
  -> set exponential next-attempt backoff
```

Backoff starts at 5 seconds and is capped at 5 minutes.

## Heartbeat

When configured, heartbeat POST contains:

```json
{
  "station_id": "WHD-NC",
  "version": "...",
  "git_sha": "...",
  "at_utc": "..."
}
```

A successful heartbeat is recorded in the operational audit stream.

## Status

```text
GET /api/central/status
```

Reports whether ticket sync / heartbeat are enabled and the most recent attempt, ACK, heartbeat, and error timestamps. Secrets are never returned.

## Security boundary

- Bearer token is supplied only as runtime configuration.
- Token is not logged, persisted into business records, returned by API, or written to audit.
- TLS/auth policy of the Central endpoint is deployment infrastructure responsibility.
- Central failure must never block the local safety path.
