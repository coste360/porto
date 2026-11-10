---
title: Errors and conventions
description: RFC 9457 errors, idempotency, optimistic concurrency, pagination and the offline queue.
sidebar:
  order: 3
---

## Errors

Every error is an `application/problem+json` document (RFC 9457):

```json
{
  "type": "version_conflict",
  "title": "Version conflict",
  "status": 412,
  "request_id": "…"
}
```

The `type` field is a **stable slug**: rely on it in code, not on the text. The panel translates it with the key
`errors.<slug>`. Validation errors list the invalid fields.

## Idempotency

Creating `POST`s accept the `Idempotency-Key` header. The same key with the same body returns the stored response
without repeating the operation; with a different body the answer is `422`, while the first request is still running
`409`. Keys last 24 hours per user or API key (7 days for offline-queue operations). Use a new key (a UUID, for
example) for each logical operation and reuse it only to retry it.

## Concurrency

Versioned resources return `ETag: "<version>"`. Changes (`PATCH`) require `If-Match` with that version: if someone
changed the resource in the meantime, the answer is `412 version_conflict` and you must read it again; without
`If-Match` the answer is `428`.

Occupancies (berths, cells of dynamic areas, land resources, rental units, lifting equipment) are protected by unique
database constraints: two concurrent requests for the same space on the same night can never both succeed, whatever
the channel (desk, public page, API, external channel).

## Pagination

Collections use an opaque cursor: `?limit=` (default 50, max 200) and `?cursor=` with the `next_cursor` of the
previous response; without `next_cursor` the collection is over.

## Offline queue

Only operations marked `x-porto-offline: true` in the contract (arrivals and departures, photos, meter readings,
worked hours, dock payments and a few others) can be replayed by the panel's offline queue. A replay carries
`Idempotency-Key` and `Porto-Offline-Queued-At` (the instant the operation was queued); the same header on any other
operation is refused with `422 offline_not_allowed`.

## Sizes

A request body is limited to 1 MiB, except for operations that declare another limit (`x-porto-max-body-bytes`, at
most 16 MiB for uploads).
