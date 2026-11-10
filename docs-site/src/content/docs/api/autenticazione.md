---
title: Autenticazione
description: Sessioni del pannello, chiavi API con scope, token dei dispositivi e permessi.
sidebar:
  order: 2
---

## Chiavi API

Per le integrazioni crea una **chiave API** nel pannello (*Chiavi API*). La chiave inizia con `po_live_`, viene
mostrata **una sola volta** alla creazione ed è salvata solo come hash. Si usa nell'intestazione `Authorization`:

```bash
curl -H "Authorization: Bearer po_live_…" https://porto.mio-marina.it/api/admin/v1/meta
```

- Gli **scope** si scelgono alla creazione e sono un sottoinsieme dei permessi di chi la crea; una chiave non può mai
  gestire gli utenti.
- Una chiave si revoca dal pannello in qualsiasi momento.
- Le richieste con `Authorization: Bearer` non usano cookie e non richiedono il token CSRF.

## Sessioni del pannello

Il pannello usa il cookie `porto_session` (HttpOnly, `Secure`, SameSite=Lax). Le richieste che modificano dati
devono inviare l'intestazione `X-CSRF-Token` con il valore del cookie `porto_csrf`. Le sessioni scadono dopo 24 ore
di inattività e comunque dopo 14 giorni; il cambio della password revoca le altre sessioni.

## Dispositivi

Telefoni e app desktop in modalità remota si abbinano con un codice monouso o un QR (*Dispositivi*) e ricevono un
**token di dispositivo** `po_dev_…`, legato a un utente e a un marina. Il ruolo si rilegge a ogni richiesta: togliere
l'utente dal marina revoca anche il dispositivo.

## Permessi

I permessi sono legati ai ruoli (titolare, direzione, ufficio, amministrazione, ormeggiatore, capo cantiere, operaio,
addetto al noleggio, sola lettura) e sono verificati dal server su ogni operazione. Il riepilogo di ogni operazione
nel [riferimento](/api/riferimento/) indica tra parentesi il permesso richiesto, per esempio `(contracts.read)`.
Vedi [Team e sicurezza](/gestore/team-e-sicurezza/).

## Limiti

L'accesso è limitato per account (5 errori in 15 minuti) e per indirizzo; l'API pubblica ha un limite per rete di
provenienza (`PORTO_PUBLIC_READS_PER_MINUTE`). Oltre il limite la risposta è `429` con `Retry-After`.
