---
title: Cantiere
description: Catalogo con formule, preventivo guidato, commesse, planner dei dipendenti, ore, materiali, consuntivo e mezzi di sollevamento.
sidebar:
  order: 15
---

## Catalogo

*Catalogo cantiere* contiene servizi, manodopera (con tariffa oraria per qualifica) e materiali, e i **gruppi di
lavorazioni** (per esempio "Antivegetativa" = alaggio + lavaggio + carteggiatura + due mani + varo).

Ogni riga può avere una **formula** di quantità o di prezzo che usa le misure della barca (lunghezza, larghezza,
pescaggio, motori, tipo di scafo…), variabili derivate come la superficie dell'opera viva e variabili chieste
all'operatore (numero di mani, area del danno). Le formule hanno funzioni come `if`, `min`, `max`, `round` e tabelle a
fasce; una formula sbagliata è segnalata al salvataggio, con un caso di prova.

## Preventivo guidato

*Preventivi cantiere*: 1) barca e cliente, 2) gruppi di lavorazioni, servizi e prodotti dal catalogo, 3) variabili
richieste dalle formule, 4) riepilogo con quantità e prezzi **calcolati**, modificabili riga per riga (le modifiche
manuali restano segnate), 5) condizioni, validità e acconto. Si invia con PDF e link; il cliente accetta dal link o in
ufficio, e l'accettazione crea la **commessa** con attività e acconto.

## Commesse

*Barche in lavorazione* mostra le commesse in bacheca (per stato) o in elenco: pianificata, in corso, sospesa,
completata, consegnata, fatturata. Ogni commessa ha attività con qualifica, ore stimate, dipendenze, checklist e foto;
l'avanzamento è la media delle attività pesata sulle ore.

## Planner e carico di lavoro

- *Dipendenti*: qualifiche, costo orario interno, orari settimanali, assenze, reparto; un dipendente può avere un utente
  per il telefono.
- *Planner cantiere*: dipendenti × giorni, con le attività trascinabili e il controllo di qualifica e capacità; una
  proposta automatica assegna le attività non pianificate, da confermare.
- *Carico di lavoro*: ore assegnate su ore disponibili per dipendente, reparto e qualifica, con sovraccarichi e commesse
  a rischio.

## Ore, materiali e consuntivo

- **Ore lavorate** con il timer anche dal telefono e senza rete, oppure inserite a mano; *Approvazione ore* le conferma
  e ne congela il costo.
- **Materiali** scaricati e resi sulla commessa; **costi esterni** con importo, riferimento e ricarico.
- **Consuntivo**: ricavi e costi di ore, materiali e costi esterni, confronto con il preventivo e **margine**.
- **Addebiti** di acconto, stato di avanzamento, finale e conguaglio, che vanno nell'esportazione "da fatturare".

Il magazzino del cantiere si limita ai materiali scaricati sulle commesse: una gestione completa delle giacenze non è
inclusa.

## Mezzi di sollevamento

*Mezzi di sollevamento*: travel lift, gru, muletto e carrello prenotati a fasce di 15 minuti con operatore abilitato,
**senza sovrapposizioni** (garantito dal database). Alaggi e vari si prenotano dalla commessa, dal rimessaggio o dallo
spostamento acqua ↔ terra; il calendario del giorno si vede anche dal telefono.
