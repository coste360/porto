---
title: Dati dimostrativi
description: Il porto dimostrativo di porto seed demo e l'istanza demo pubblica con azzeramento notturno.
sidebar:
  order: 10
---

`porto seed demo` crea l'organizzazione "Porto Demo" con il marina "Marina Demo" (slug `demo`), un utente per ogni
ruolo (`<ruolo>@demo.porto.test`, stessa password per tutti) e un anno intero di attività, scritto con gli stessi
servizi usati dal pannello. Le date sono relative al giorno corrente del marina e ogni parte è idempotente: una
seconda esecuzione non aggiunge nulla.

```bash
PORTO_ADMIN_PASSWORD='scegli-una-password-demo' porto seed demo     # oppure --password-stdin
make seed                                                            # database di sviluppo in tmp/data
docker compose exec porto /porto seed demo                           # con il compose di prova
```

Senza password ne viene generata una, mostrata una sola volta. Il caricamento richiede circa un minuto su SQLite.

## Cosa contiene

| Area | Contenuto |
|---|---|
| Porto e anagrafiche | 4 mappe, 350 posti, 250 clienti, 260 barche con documenti |
| Contratti | circa 150 contratti annuali, circa 140 transiti intorno a oggi, campagna di rinnovo per l'anno dopo, diritti d'uso, spese comuni |
| Anno precedente | contratti, transiti e incassi dell'anno scorso, per i confronti del cruscotto |
| Addebiti | addebiti, esportazioni, incassi, sessioni di cassa, solleciti |
| Prenotazione online | pagina `/b/demo` con 7 prenotazioni in stati diversi (in approvazione, approvate in attesa del bonifico, confermate, rifiutate) |
| Rimessaggio | contratti in capannone e su piazzale collocati dall'ottimizzatore |
| Cantiere | 10 preventivi, 8 commesse in tutti gli stati, planner dei dipendenti, alaggi e vari con travel lift e gru |
| Noleggio | 10 unità, 3 skipper, circa 30 prenotazioni (uscite in corso, rientri, opzioni, cancellazioni) |
| Sensori | 43 sensori su un pontile e un'area lineare, 6 ore di letture, un avviso aperto per tipo |

Le email dei clienti dimostrativi usano il dominio riservato `.test`: nessun messaggio può raggiungere qualcuno.

## Demo pubblica con azzeramento notturno

Per una demo pubblica che si ricarica ogni notte c'è la ricetta in
[`deploy/demo`](https://github.com/coste360/porto/tree/main/deploy/demo): `reset.sh` ferma il servizio, esegue
`porto seed demo --reset` con `PORTO_DEMO_INSTANCE=true`, riavvia e aspetta `/readyz`.

`--reset` cancella database, file caricati ed email e ricarica la demo. Parte solo con `PORTO_DEMO_INSTANCE=true` e
rifiuta un database che contiene altro oltre all'organizzazione dimostrativa.

:::danger
Non impostare mai `PORTO_DEMO_INSTANCE` su un'istanza reale e non usare la ricetta demo per un porto vero: la password
della demo è pubblica e ogni modifica viene persa al reset.
:::
