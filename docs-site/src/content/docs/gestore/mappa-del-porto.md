---
title: Mappa del porto
description: Disegnare il porto in scala con l'editor - sfondo calibrato, generatori, validazioni, gruppi e categorie.
sidebar:
  order: 2
---

La mappa è il cuore di Porto: ogni posto, pontile, area e capannone è misurato in **centimetri** e ogni barca occupa il
suo ingombro reale. Si configura in *Mappe* (permesso di configurazione del marina).

## Più mappe per marina

Puoi avere più mappe: specchio acqueo, piazzale, capannone, parcheggi e box. Ognuna ha i suoi elementi:

- **posti barca** su pontili e banchine, con finger o a corpo morto, posti in testata, **campi boe**;
- **aree dinamiche**: un lato di pontile o un tratto di banchina dove le barche si affiancano secondo la lunghezza;
- **superfici** di piazzale e capannone a celle, con varchi e corsie, per il rimessaggio;
- **risorse a terra**: posti auto, box, magazzini, armadietti, invasi e carrelli;
- **colonnine** con prese (energia, acqua) e contatori, decorazioni e servizi.

## Sfondo e scala

1. Carica un'ortofoto o una planimetria come sfondo.
2. Indica due punti e la loro distanza reale: l'editor calcola la scala.
3. Ruota lo sfondo finché il nord coincide; se vuoi, inserisci punti con coordinate geografiche per la
   georeferenziazione.

Lo sfondo non è obbligatorio: la vista **a schema** funziona anche senza.

## Generatori

I generatori creano decine di posti in un colpo, con anteprima prima di applicarli:

- **pontile** con posti su uno o due lati, anche a scalare verso la testata, finger ogni N posti o corpo morto, posti
  in testata, numerazione e prefisso;
- **posti lungo una banchina**;
- **area dinamica** su un lato di pontile o un tratto di banchina;
- **campo boe** a griglia o a quinconce, con il raggio di rotazione;
- **griglia di posti auto e box**;
- **piazzale o capannone** a celle con varchi e corsie.

L'editor ha aggancio alla griglia e alle strutture, selezione multipla, allinea e distribuisci, duplica, rinumera,
annulla e ripeti.

## Validazioni

Al salvataggio Porto controlla la mappa: **blocca** codici duplicati e posti che si sovrappongono e **avvisa** per i
posti sotto la misura minima, senza fondale o senza categoria di prezzo. Un posto con occupazioni future non si
elimina ma si disattiva, e Porto elenca contratti e transiti da spostare; ridimensionarlo richiede la verifica delle
barche assegnate.

## Gruppi e categorie

- I **gruppi** (per esempio "Pontile B" o "Transiti") raccolgono posti, aree e risorse e si usano in listini, quote
  online, report, filtri e comunicazioni. Un posto può stare in più gruppi.
- Le **categorie di prezzo** si assegnano per posto o in blocco dalla selezione.

## Importazione e stampa

Puoi importare l'elenco dei posti da CSV (codice, pontile, misure, fondale, categoria), utile se arrivi da un altro
gestionale, e stampare la mappa in scala con la legenda. Ogni salvataggio crea una nuova versione della mappa.
