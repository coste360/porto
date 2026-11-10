---
title: Contracts and transits
description: Inquiries, quotes with berth options, mooring contracts, transits, charters, instalments and waiting list.
sidebar:
  order: 6
---

## Contract types

| Type | Use |
|---|---|
| **Mooring**: annual, seasonal, monthly or period | a berth (or space in a dynamic area) for a period, with an instalment plan |
| **Transit** | stay by nights, from the desk, the online page, the API or a channel |
| **Dry storage** | boat on land in a yard or shed ([Dry storage](/en/gestore/rimessaggio/)) |
| **Charter** | a charter company's fleet rotating over a set of berths for the season |

Each contract has a sequential number per marina, type and year, keeps the price snapshot and can include services
(parking space, flat-rate utilities). Every state change is recorded in the activity log.

## From inquiry to contract

1. **Inquiry** (*Richieste*): every request for a berth (phone, email, desk, online) with boat or sizes, period and
   outcome. Unmet inquiries feed the lost demand report.
2. **Quote** (*Preventivi*): created from the search in *Vista operativa* or from the customer card, with up to three
   price alternatives, validity and terms. Optionally it holds the berth **as an option** until it expires. It is sent
   by email with a PDF and a link.
3. **Acceptance**: from the panel or from the customer's link, where the customer picks the alternative, completes the
   data, signs and pays the deposit. Porto checks again that the berth is free, creates the contract and the instalment
   plan and, when needed, the signature request.
4. **Activation**: on signature (or on the "signed on paper" confirmation) the contract becomes active and the charge
   for the first instalment or the deposit is created.

Contract states: draft → awaiting signature → active → expired, renewed or terminated; it can be suspended for arrears
according to the marina's rules.

## During the contract

- **Addendum**: added service, parking space, price change, with its own effective date, document and signature; the
  original contract does not change.
- **Change of vessel** with a fit check and a new price if the price depends on the boat.
- **Withdrawal** with a date, refund or penalty calculation and an adjustment charge.
- **Night release**: an annual contract can release the nights when the boat is away, which the marina can resell to
  transits under the contract's rules.

## Transits

A transit occupies the nights from arrival to departure, excluded. Main states: held → pending payment → confirmed →
arrived → departed, or cancelled, no-show, expired or awaiting approval (for online bookings that require it). From the
card you extend or shorten the stay, with a new price and an additional or adjustment charge. At the desk a transit can
be recorded and paid at departure in one step.

## Charters

A charter contract is in the name of a company and covers its fleet on a set of berths for the season, with a changeover
day and nights with boats away. The fleet's boats rotate over the contract's berths with no new contracts.

## Instalments

The **instalment plan** comes from the payment terms (deposit, instalments on fixed dates or at intervals, SEPA direct
debit) and always adds up exactly to the total. On its issue date each instalment becomes a charge, with the payment
request if configured. *Scadenzario* (Instalments) shows instalments by period and state, with bulk actions (charge now,
remind).

## Waiting list

*Lista d'attesa* holds requests for an annual berth by size and group, in date order. When a compatible berth frees up,
Porto proposes the first in the list to the office.
