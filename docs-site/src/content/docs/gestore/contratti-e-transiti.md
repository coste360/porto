---
title: Contratti e transiti
description: Richieste, preventivi con opzione sul posto, contratti di ormeggio, transiti, charter, rate e lista d'attesa.
sidebar:
  order: 6
---

## Tipi di contratto

| Tipo | Uso |
|---|---|
| **Ormeggio** annuale, stagionale, mensile o di periodo | un posto (o uno spazio in un'area dinamica) per un periodo, con piano rate |
| **Transito** | soggiorno a notti, dal banco, dalla pagina online, dall'API o da un canale |
| **Rimessaggio** | barca a terra su piazzale o in capannone ([Rimessaggio](/gestore/rimessaggio/)) |
| **Charter** | una flotta di una società di charter che ruota su un insieme di posti per la stagione |

Ogni contratto ha un numero progressivo per marina, tipo e anno, conserva lo snapshot del prezzo e può includere
servizi (posto auto, utenze forfettarie). Ogni cambio di stato è registrato nel registro attività.

## Dalla richiesta al contratto

1. **Richiesta** (*Richieste*): ogni domanda di posto (telefono, email, banco, online) con barca o misure, periodo ed
   esito. Le richieste non evase alimentano il report della domanda persa.
2. **Preventivo** (*Preventivi*): nasce dalla ricerca in *Vista operativa* o dalla scheda del cliente, con fino a tre
   alternative di prezzo, validità e condizioni. Facoltativamente tiene il posto **in opzione** fino alla scadenza.
   Si invia per email con PDF e link.
3. **Accettazione**: dal pannello o dal link del cliente, che sceglie l'alternativa, completa i dati, firma e paga
   l'acconto. Porto ricontrolla che il posto sia libero, crea il contratto e il piano rate e, se serve, la richiesta
   di firma.
4. **Attivazione**: alla firma (o alla conferma "firmato su carta") il contratto diventa attivo e nasce l'addebito
   della prima rata o dell'acconto.

Stati del contratto: bozza → in attesa di firma → attivo → scaduto, rinnovato o risolto; può essere sospeso per
morosità secondo le regole del marina.

## Durante il contratto

- **Addendum**: servizio aggiunto, posto auto, variazione di prezzo, con decorrenza, documento e firma propri; il
  contratto originale non cambia.
- **Cambio di imbarcazione** con verifica di compatibilità e ricalcolo del prezzo se dipende dalla barca.
- **Recesso** con data, calcolo di rimborso o penale e addebito di rettifica.
- **Rilascio di notti**: un contratto annuale può liberare le notti in cui la barca è fuori, che il porto può
  rivendere ai transiti secondo le regole del contratto.

## Transiti

Il transito occupa le notti dall'arrivo alla partenza esclusa. Stati principali: in opzione → in attesa di pagamento →
confermato → arrivato → partito, oppure cancellato, mancato arrivo, scaduto o in attesa di approvazione (per le
prenotazioni online che lo richiedono). Dalla scheda si allunga o si accorcia il soggiorno, con ricalcolo e addebito
integrativo o di rettifica. Al banco il transito si può registrare e incassare alla partenza in un solo passaggio.

## Charter

Il contratto charter è intestato a una società e copre la sua flotta su un insieme di posti per la stagione, con giorno
di cambio e notti di barca fuori. Le barche della flotta ruotano sui posti del contratto senza nuovi contratti.

## Rate e scadenzario

Il **piano rate** nasce dalle condizioni di pagamento (acconto, rate a date fisse o a intervalli, addebito SEPA) e somma
sempre esattamente al totale. Alla data di emissione ogni rata diventa un addebito, con la richiesta di pagamento se
configurata. *Scadenzario* mostra le rate per periodo e stato, con azioni in blocco (addebita ora, sollecita).

## Lista d'attesa

*Lista d'attesa* raccoglie le richieste di posto annuale per dimensioni e gruppo, in ordine di data. Quando si libera un
posto compatibile, Porto propone all'ufficio il primo della lista.
