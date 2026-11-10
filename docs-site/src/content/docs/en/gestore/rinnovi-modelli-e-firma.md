---
title: Renewals, templates and signature
description: Single renewals and renewal campaigns, print templates, paper, OTP and on-screen signature, communications.
sidebar:
  order: 7
---

## Renewals

- **Single renewal**: from the active contract, *Rinnova il contratto* creates the next contract as a draft with the
  same berth, boat and services, the new price list's price and the berth held for the holder until the deadline.
- **Renewal campaign** (*Rinnovi*): 1) filter (group, contract type, expiry), 2) preview with old and new prices and
  exclusions (for example customers in arrears or with expired insurance), 3) message and deadline, 4) start. Each
  customer receives the proposal by email with a link to accept, sign and pay the deposit online. Progress is shown live.
- **Tacit renewal**, when the contract provides for it: on expiry Porto creates the new contract and notifies the
  customer.

## Print templates

*Modelli di stampa* holds the document templates (contracts, quotes, addenda, handover reports, receipts…) per type and
language, written in Markdown with **merge fields** (customer, boat, berth, period, price, instalments…). Porto renders
the PDF itself. The default templates apply until you publish your own.

## Signature

*Firme* lists signature requests with signers and their steps. The included methods are:

- **on paper**: print, have it signed, upload the scan (PDF, PNG or JPEG, encrypted) and confirm "signed on paper";
- **with OTP**: the signer receives the link by email, confirms their identity and signs with a one-time code sent by
  email;
- **on screen**: the customer signs on a tablet or phone, for example at the desk or at a rental check-out.

For every signature Porto keeps the evidence (document fingerprint, declared identity, timestamps). Advanced or
qualified e-signature from an external provider is an extension point: no provider is included
([Extensions](/en/sviluppo/estensioni/)).

## Communications

*Comunicazioni* emails customers chosen by group, pier, contract type, presence or map selection: recipients, content,
attachments, preview and sending. It respects consents, includes one-click unsubscribe and keeps a log of what was sent.

## Automatic deadlines

The deadline engine warns about contracts, policies, documents and concession obligations about to expire
([Customers and vessels](/en/gestore/clienti-e-imbarcazioni/#documents-and-deadlines)).
