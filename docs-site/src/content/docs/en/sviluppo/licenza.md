---
title: Licence and trademark
description: Business Source License 1.1 with free use for marinas, conversion to AGPL-3.0 after three years, reserved trademark.
sidebar:
  order: 7
---

Porto is **source available** under the
**[Business Source License 1.1](https://github.com/coste360/porto/blob/main/LICENSE)** (SPDX `BUSL-1.1`). The binding
text is the `LICENSE` file; this summary does not replace it.

## In practice

- **Free for marinas you run.** You may use Porto in production, at no cost, for the marinas, tourist ports, berthing
  points (buoy fields included), boatyards and dry-storage sites you own or operate directly, also through controlled
  companies, with the ancillary services of the same site, and publish the booking page and the widget for your
  customers.
- **Free to try and improve.** You may read, copy, modify and test the code and contribute.
- **Not for resale.** Offering Porto or a modified version to third parties for a fee or other commercial benefit
  (hosting, managed service or SaaS, installation, configuration, customisation, maintenance, support, resale), or
  including it in a competing product, needs a **commercial licence** from Coste360 SRL: write to info@coste360.it.
- **Becomes AGPL-3.0 after 3 years.** Each version converts to the
  [GNU AGPL-3.0 or later](https://www.gnu.org/licenses/agpl-3.0.html) three years after its first public release.

## Practical cases

- **Direct operation.** You operate a facility directly if you, or a company of your group, hold its concession (for
  example the state maritime concession), licence or title, or you run it on your own account and not as a supplier
  of someone else. The ancillary services of the same site are included, such as berthing, fuel, rentals, boatyard
  work, bar and restaurant.
- **Agencies, technicians and software houses.** Installing, configuring, customising, hosting, maintaining or
  supporting Porto for other marinas for a fee requires a commercial licence. The operator can install it themselves,
  with their own employees or on a server they rent.
- **Consortia and associations.** Each member marina uses Porto for free for itself; a consortium or association that
  does not control its member marinas does not operate them directly and needs a commercial licence to offer Porto to
  its members.
- **Websites.** Embedding the booking page or the widget in a website or an app needs no licence, as long as whoever
  does it does not install, host, configure, customise, maintain or support Porto.
- **Free help.** Helping an operator for free is allowed, as long as the help does not serve to sell or promote a paid
  offer tied to Porto or to the marina's operations, bookings or website.
- **Installation templates.** A generic or "one-click" template, with which the operator starts Porto by themselves on
  a server they rent, is allowed if the provider does not run Porto for them and does not sell an offer dedicated to
  Porto.

The decision and its reasons are in [ADR 0018](https://github.com/coste360/porto/blob/main/docs/adr/0018-licenza.md)
(Italian); more questions in the [FAQ](/en/installazione/domande-frequenti/).

## Trademark

The **Porto** name, the logo and the files in `assets/brand/porto/` belong to Coste360 SRL, all rights reserved, and
are **not covered by the code licence**. A fork must have its own name and logo. The usage rules are in
[ADR 0007](https://github.com/coste360/porto/blob/main/docs/adr/0007-marchio.md) and the
[trademark policy](https://github.com/coste360/porto/blob/main/TRADEMARK.md).

## Dependencies

Porto's dependencies have compatible licences (MIT, BSD, Apache-2.0, ISC, MPL-2.0; OFL-1.1 for the Titillium Web
font), checked by `make licenses`.
