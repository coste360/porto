---
title: Errori e convenzioni
description: Errori RFC 9457, idempotenza, concorrenza ottimistica, paginazione e coda offline.
sidebar:
  order: 3
---

## Errori

Ogni errore è un documento `application/problem+json` (RFC 9457):

```json
{
  "type": "version_conflict",
  "title": "…",
  "status": 412,
  "detail": "…",
  "request_id": "…"
}
```

Il campo `type` è uno **slug stabile**: usalo nel codice, non il testo. Il pannello lo traduce con la chiave
`errors.<slug>`. Gli errori di validazione elencano i campi non validi.

## Idempotenza

Le `POST` di creazione accettano l'intestazione `Idempotency-Key`. La stessa chiave con lo stesso corpo restituisce
la risposta già salvata, senza ripetere l'operazione; con un corpo diverso la risposta è `422`, con la prima
richiesta ancora in corso `409`. Le chiavi valgono 24 ore per utente o chiave API (7 giorni per le operazioni della
coda offline). Usa una chiave nuova (per esempio un UUID) per ogni operazione logica e riusala solo per ripeterla.

## Concorrenza

Le risorse versionate restituiscono `ETag: "<versione>"`. Le modifiche (`PATCH`) richiedono `If-Match` con quella
versione: se nel frattempo qualcuno ha cambiato la risorsa, la risposta è `412 version_conflict` e bisogna rileggerla; senza `If-Match` la risposta è `428`.

Le occupazioni (posti, celle delle aree dinamiche, risorse a terra, unità a noleggio, mezzi di sollevamento) sono
protette da vincoli unici del database: due richieste contemporanee sullo stesso spazio nella stessa notte non
possono riuscire entrambe, qualunque sia il canale (banco, pagina pubblica, API, canale esterno).

## Paginazione

Le collezioni usano un cursore opaco: `?limit=` (predefinito 50, massimo 200) e `?cursor=` con il valore
`next_cursor` della risposta precedente; senza `next_cursor` la collezione è finita.

## Coda offline

Solo le operazioni marcate `x-porto-offline: true` nel contratto (arrivi e partenze, foto, letture dei contatori,
ore lavorate, incassi in banchina e poche altre) possono essere ripetute dalla coda offline del pannello. La
ripetizione porta `Idempotency-Key` e `Porto-Offline-Queued-At` (l'istante in cui l'operazione è stata messa in
coda); la stessa intestazione su un'altra operazione è rifiutata con `422 offline_not_allowed`.

## Dimensioni

Il corpo di una richiesta è limitato a 1 MiB, salvo le operazioni che dichiarano un limite diverso
(`x-porto-max-body-bytes`, al massimo 16 MiB per i caricamenti).
