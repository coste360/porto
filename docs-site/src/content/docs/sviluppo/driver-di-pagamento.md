---
title: Driver di pagamento
description: Il driver manuale, il driver Stripe e come si aggiunge un gateway con PaymentProvider.
sidebar:
  order: 5
---

Porto funziona **senza alcun pagamento online**: il driver `manual` (bonifico, contanti, POS, assegno) è sempre
attivo. I gateway online sono facoltativi e si attivano per marina.

## Driver inclusi

- **`manual`**: incassi registrati dall'ufficio, dalla cassa o dalla banchina; richieste di pagamento con le coordinate
  bancarie. Un transito online in attesa di bonifico si conferma registrando l'incasso.
- **`stripe`**: pagina di pagamento ospitata da Stripe (carta, wallet, addebito SEPA), mandati SEPA per le rate,
  addebiti sui mandati, rimborsi, verifica dello stato e preautorizzazione con cattura successiva (per le
  prenotazioni online da approvare). Usa le chiavi **del gestore**: nessuna commissione va alla piattaforma.

## Configurare Stripe

Nel pannello, *Pagamenti online*: chiave segreta, chiave pubblicabile, segreto del webhook, metodi e mandati SEPA.
Le chiavi sono conservate cifrate e non vengono più mostrate. Il pannello indica l'**URL del webhook** da
configurare in Stripe, nella forma `https://<host>/webhooks/payments/stripe/<id del marina>`, e permette di provare
la connessione.

## Regole

- **Un incasso online si conferma solo dal webhook firmato** del provider, mai dal ritorno del browser alla pagina di
  Porto. Il webhook è idempotente: ricevuto due volte, registra l'incasso una volta.
- Un pagamento arrivato dopo la scadenza del blocco del posto recupera lo stesso posto se è ancora libero, altrimenti
  viene rimborsato per intero.
- I metadati inviati al provider contengono solo l'id del marina e dell'intento di pagamento; la descrizione contiene
  i numeri degli addebiti, mai dati personali.

## Un nuovo gateway

Un gateway implementa `ext.PaymentProvider` (`pkg/ext/payment.go`): pagina di pagamento ospitata, eventuali mandati di
addebito diretto, rimborsi, stato e `ParseWebhook`, che verifica la firma del provider con il segreto del marina e
restituisce un evento di pagamento oppure `ErrIgnoredEvent` per i tipi che Porto non usa. La preautorizzazione è
facoltativa (`PaymentAuthorizer`). Le credenziali arrivano per chiamata nel contesto; un driver non le salva e non le
scrive nei log. Prima di iniziare apri una issue *nuovo driver*.
