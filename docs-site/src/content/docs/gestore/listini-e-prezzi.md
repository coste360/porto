---
title: Listini e prezzi
description: Articoli, classificazioni, listini per periodi, basi di calcolo, simulatore, matrice e rinnovo del listino.
sidebar:
  order: 5
---

Tutti i prezzi di Porto (ormeggi, transiti, rimessaggio, servizi, consumi, cantiere, noleggio) escono dallo stesso
**motore dei prezzi**: dati gli stessi input dà sempre lo stesso risultato, al centesimo.

## Articoli e servizi

*Articoli e servizi* elenca tutto ciò che si vende o si addebita: codice, descrizione, tipo, unità (notte, mese,
stagione, anno, metro, m², kWh, m³, ora, pezzo, forfait), **aliquota IVA** o natura dell'esenzione, prezzo IVA inclusa o
esclusa, **classe di ricavo** e codice nel software di fatturazione esterno. Questi dati finiscono nell'esportazione
"da fatturare".

*Classificazioni* definisce categorie dei posti, fasce di lunghezza e le altre classificazioni usate dalle regole.

## Listini

Un listino ha un periodo di validità e uno stato (bozza, attivo, archiviato). Contiene:

- **periodi tariffari** (alta, media, bassa stagione, eventi);
- **regole di prezzo** con un ambito (articolo, tipo di contratto, gruppo o categoria di posto, canale, categoria del
  cliente) e una **base di calcolo**: per posto, per fascia di lunghezza, al metro, al m² della barca, al m² del posto,
  importo fisso o a consumo;
- **frazionamenti** per i periodi non interi: esatto, pro rata, mesi interi o la combinazione più conveniente;
- **maggiorazioni e sconti** (multiscafo, sbalzo, soggiorno lungo, prenotazione anticipata, ultimo minuto, canale),
  servizi inclusi e aggiuntivi, arrotondamenti e acconti.

Un listino **attivato non si modifica più**: ogni contratto e transito conserva lo snapshot del prezzo e resta
ricalcolabile. Per cambiare i prezzi si crea un nuovo listino.

## Strumenti

- **Simulatore prezzi**: inserisci barca, periodo e posto e vedi il prezzo con il dettaglio riga per riga.
- **Matrice**: la tabella dei prezzi del listino per fascia e periodo, anche da stampare; coincide sempre con il
  simulatore.
- **Copertura**: segnala le combinazioni che non hanno un prezzo, prima che succeda al banco.
- **Rinnovo del listino**: crea il listino dell'anno dopo da quello attuale, con una variazione, e mostra le
  differenze prima di attivarlo.

*Impostazioni prezzi* raccoglie le scelte generali del marina (arrotondamenti, IVA predefinita e simili).
