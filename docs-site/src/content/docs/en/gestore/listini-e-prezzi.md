---
title: Price lists and pricing
description: Items, classifications, price lists by period, pricing bases, simulator, matrix and price list renewal.
sidebar:
  order: 5
---

Every price in Porto (moorings, transits, dry storage, services, utilities, boatyard, rentals) comes from the same
**pricing engine**: with the same inputs it always gives the same result, to the cent.

## Items and services

*Articoli e servizi* lists everything that is sold or charged: code, description, type, unit (night, month, season,
year, metre, m², kWh, m³, hour, piece, lump sum), **VAT rate** or exemption nature, price including or excluding VAT,
**revenue class** and code in the external invoicing software. These data go into the "to invoice" export.

*Classificazioni* defines berth categories, length bands and the other classifications used by the rules.

## Price lists

A price list has a validity period and a state (draft, active, archived). It contains:

- **rate periods** (high, mid, low season, events);
- **price rules** with a scope (item, contract type, berth group or category, channel, customer category) and a
  **pricing basis**: per berth, per length band, per metre, per m² of the boat, per m² of the berth, flat or metered;
- **splitting** for partial periods: exact, pro rata, whole months or the cheapest combination;
- **surcharges and discounts** (multihull, overhang, long stay, early booking, last minute, channel), included and
  additional services, rounding and deposits.

An **activated price list never changes**: every contract and transit keeps its price snapshot and can be recomputed.
To change prices you create a new price list.

## Tools

- **Price simulator**: enter boat, period and berth and see the price with its line-by-line breakdown.
- **Matrix**: the price table of the list by band and period, printable too; it always matches the simulator.
- **Coverage**: flags combinations without a price, before it happens at the desk.
- **Price list renewal**: creates next year's list from the current one, with a change, and shows the differences
  before activation.

*Impostazioni prezzi* (Pricing settings) holds the marina's general choices (rounding, default VAT and similar).
