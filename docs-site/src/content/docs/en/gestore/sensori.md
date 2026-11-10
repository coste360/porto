---
title: Occupancy sensors
description: Physical berth state from sensors, reconciliation with contracts, alerts and proposals to confirm.
sidebar:
  order: 17
---

Sensors tell which berths are **physically** occupied. Porto compares this state with the expected occupancy and helps
you act: resell the berths of boats that are away, catch unregistered arrivals, check transit arrivals.

## Devices

*Sensori* gathers berth states, alerts and proposals, **devices**, **gateways** and **sources**. A device is linked to a
berth, a stretch of a dynamic area or a pedestal; on creation you get its **key**, shown once, to load on the sensor.
Porto is not tied to a vendor: any sensor or gateway following the [open protocol](/en/api/ingestione-sensori/) works.

## Physical state

A berth becomes occupied or free only after agreeing readings (by default 2 readings or 5 minutes), so waves are not
mistaken for an arrival. A silent sensor becomes "unknown". Thresholds are changed in the sensor settings. The *Sensori*
lens of the map shows free, occupied, discrepancies and sensors offline or with low battery.

## Reconciliation

| Situation | What Porto proposes |
|---|---|
| Berth occupied with no expected occupancy | unexpected occupied berth alert |
| Transit expected today and the berth becomes occupied | recording the arrival |
| Annual contract berth free for a long time | presence "away" and, if the contract allows it, releasing the nights |
| Released and resold berth occupied before the transit arrives | early return alert |
| Silent sensor or low battery | a maintenance task |

**Sensors propose, they do not decide**: no reading changes contracts or occupancies without an operator's confirmation.
The only automatic effect, if you enable it, is on presence.

## History and report

Each sensor's history shows the physical state night by night compared with the contractual one; the *Sensori* report
summarises discrepancies. Raw readings are kept for 30 days (configurable), then only state changes remain.
