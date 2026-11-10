---
title: FAQ
description: Cost, licence, invoicing, online booking, working without internet, several marinas and data protection.
sidebar:
  order: 13
---

## Is there a free management system for marinas?

Yes: Porto is a free, source-available management system for marinas, tourist ports, berthing points and boatyards.
You install it on your own server or on the office PC, with no subscription or paid licence, and it covers the harbour
map, mooring contracts and transits, online berth booking, charges and payments, dry storage, boatyard, rentals and
reports.

## Is Porto really free?

Yes, for those who run their own facility. For marinas, berthing points, boatyards or dry-storage sites you own or
operate directly there are no fees, paid editions or limits on berths, contracts or users: the only cost is the
computer or server it runs on. A commercial licence is needed only to offer Porto as a service to third parties
([Licence and trademark](/en/sviluppo/licenza/)).

## I only run a boatyard or dry storage, with no berths in the water: can I use it?

Yes. Boatyards and dry storage fall under the free use just like marinas, and you can use only the parts you need:
yard and shed map, dry-storage contracts with the optimizer, boatyard quotes and work orders, lifting equipment,
charges and payments.

## I am an agency, a technician or a software house: can I install it for my clients?

Not without a commercial licence. The free licence covers only the facilities the operator owns or operates directly:
installing, configuring, customising, hosting (hosting or SaaS), maintaining or supporting Porto for other marinas for
a fee, or reselling it, requires a commercial licence from Coste360 SRL (info@coste360.it). The operator can install it
themselves, with their own employees or on a server they rent.

Whoever builds the marina's website can add the widget or a link to the booking page without a licence, as long as they
do not install, host, configure or support Porto.

## We are a consortium or an association of marinas: can we use it for our members?

Each member marina can install and use Porto for free for itself. A consortium or association that does not control
its member marinas does not operate them directly: installing, hosting or supporting Porto for the members requires a
commercial licence from Coste360 SRL.

## Why is the licence the BSL and not an open source licence?

Because Porto must stay free for marina operators without others reselling it as a paid service to the same marinas.
The Business Source License 1.1 keeps the code readable, modifiable and open to contributions, but reserves commercial
offers to third parties to Coste360. Each version then converts to AGPL-3.0 or later three years after its first
public release. Details in [Licence and trademark](/en/sviluppo/licenza/).

## Does Porto issue electronic invoices?

No. Porto computes and keeps charges (instalments, transits, services, consumption, work orders, rentals, fees) and
exports them "to invoice" as CSV, Excel or PDF for the invoicing software you already use; invoice matching then links
the issued invoices to the charges. Porto is not a fiscal cash register either. See
[Charges and payments](/en/gestore/addebiti-e-incassi/) and
[Cash register and daily takings](/en/gestore/cassa-e-corrispettivi/).

## Are there commissions on transits booked online?

No. Online payments go through your own Stripe account and the money goes to you: you pay only the payment network
fees, no percentage to the platform. You can also leave online payments off and confirm transits at the desk, by bank
transfer or at the cash desk.

## Can I use it with my WordPress site?

Yes. The booking widget goes into a WordPress "Custom HTML" block, or into Wix, Joomla, Squarespace and any other site,
with a one-line script or an iframe; a plain link to the booking page or to the marina profile works too. See
[Online booking](/en/gestore/prenotazione-online/).

## Does it work without internet?

Yes. With the [desktop app](/en/gestore/app-desktop/) the server and the data live on the office PC; other PCs and the
mooring staff's phones connect over the marina's Wi-Fi. The phone panel queues arrivals, departures, photos, readings
and payments recorded offline and sends them when the connection is back, without duplicates
([Phone and offline](/en/gestore/telefono-e-offline/)). Internet, and so a reachable server, is needed only for online
booking, online payments and remote signature.

## Does Porto use artificial intelligence?

No. Porto contains no AI features and sends no data to AI services: document data is entered by the operator, with the
file attached and deterministic checks
([ADR 0008](https://github.com/coste360/porto/blob/main/docs/adr/0008-nessuna-intelligenza-artificiale.md)). There is
no telemetry either.

## Can I manage several marinas?

Yes. One installation manages several marinas, each with its own maps, price lists, contracts, users and booking page.
With many facilities or more than 300 berths, [PostgreSQL](/en/installazione/database/) is recommended.

## Which kinds of moorings and spaces does it support?

Fixed berths on piers and quays, with fingers or mooring lines, head berths, buoy fields, dynamic areas where boats
moor alongside by length, yards and sheds with cells for dry storage, parking spaces and boxes. Boats occupy their real
footprint and Porto checks they fit. Double berthing is not handled ([Known limits](/en/installazione/#known-limits)).

## Is my customers' data safe and GDPR compliant?

The data stays on the computer or server you choose: nobody, not even Coste360, can access it. Porto collects only the
data it needs, encrypts documents and IBANs, versions consents, supports exporting and anonymising a customer's data
and never writes emails, phone numbers or documents in clear in the logs. The marina remains the data controller.
