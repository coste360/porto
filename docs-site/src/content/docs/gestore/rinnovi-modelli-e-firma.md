---
title: Rinnovi, modelli e firma
description: Rinnovi singoli e campagne di rinnovo, modelli di stampa, firma su carta, OTP e su schermo, comunicazioni.
sidebar:
  order: 7
---

## Rinnovi

- **Rinnovo singolo**: dal contratto attivo, *Rinnova il contratto* crea il contratto successivo in bozza con lo stesso
  posto, barca e servizi, il prezzo del nuovo listino e il posto bloccato per il titolare fino al termine.
- **Campagna di rinnovo** (*Rinnovi*): 1) filtro (gruppo, tipo di contratto, scadenza), 2) anteprima con prezzi vecchi e
  nuovi ed esclusioni (per esempio morosi o polizza scaduta), 3) messaggio e termine, 4) avvio. Ogni cliente riceve la
  proposta per email con un link per accettare, firmare e pagare l'acconto online. L'andamento si segue in tempo reale.
- **Rinnovo tacito**, se previsto dal contratto: alla scadenza Porto crea il nuovo contratto e avvisa il cliente.

## Modelli di stampa

*Modelli di stampa* contiene i modelli dei documenti (contratti, preventivi, addendum, verbali, ricevute…) per tipo e
lingua, scritti in Markdown con **campi di unione** (cliente, barca, posto, periodo, prezzo, rate…). Porto genera il PDF
da sé. I modelli predefiniti valgono finché non ne pubblichi uno tuo.

## Firma

*Firme* elenca le richieste di firma con i firmatari e i loro passaggi. Le modalità incluse sono:

- **su carta**: stampi, fai firmare, carichi la scansione (PDF, PNG o JPEG, cifrata) e confermi "firmato su carta";
- **con OTP**: il firmatario riceve il link per email, conferma la propria identità e firma con un codice monouso inviato per email;
- **su schermo**: il cliente firma sul tablet o sul telefono, per esempio al banco o alla consegna di un noleggio.

Per ogni firma Porto conserva le prove (impronta del documento, identità dichiarata, istanti). La firma elettronica
avanzata o qualificata di un provider esterno è un punto di estensione: nessun provider è incluso
([Estensioni](/sviluppo/estensioni/)).

## Comunicazioni

*Comunicazioni* invia email ai clienti scelti per gruppo, pontile, tipo di contratto, presenza o selezione sulla mappa:
destinatari, contenuto, allegati, anteprima e invio. Rispetta i consensi, include la disiscrizione in un clic e tiene il
registro degli invii.

## Scadenze automatiche

Il motore delle scadenze avvisa di contratti, polizze, documenti e adempimenti della concessione in scadenza
([Clienti e imbarcazioni](/gestore/clienti-e-imbarcazioni/#documenti-e-scadenze)).
