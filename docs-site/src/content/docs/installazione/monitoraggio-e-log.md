---
title: Monitoraggio e log
description: Controlli di salute, log e registro attività.
sidebar:
  order: 12
---

## Controlli di salute

| Endpoint | Significato |
|---|---|
| `GET /healthz` | il processo è vivo |
| `GET /readyz` | il database è raggiungibile e le migrazioni sono allineate |

L'immagine Docker non ha una shell: i controlli si fanno dall'esterno (dal proxy, dalla piattaforma o da un sistema di
monitoraggio).

## Log

Porto scrive i log sullo standard error, in JSON (`PORTO_LOG_FORMAT=json`, predefinito) o in testo, con il livello di
`PORTO_LOG_LEVEL`. Con Docker si leggono con `docker compose logs porto`.

I log non contengono mai dati personali o segreti: niente email, telefoni, documenti, token, password o chiavi, nemmeno
nel log delle richieste.

## Registro attività

Ogni scrittura fatta dal pannello, dall'API o dai job lascia una riga nel **registro di audit**, consultabile nel
pannello in *Registro attività* da chi ha i permessi di amministrazione.

## Email

Le email partono da una coda interna: un errore del server SMTP non blocca l'operazione che le ha generate e l'invio
viene ritentato. Senza `PORTO_SMTP_URL` le email finiscono come file `.eml` in `PORTO_MAIL_DIR`, se impostata,
altrimenti solo nel log (senza destinatario).
