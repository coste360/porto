---
title: Cassa e corrispettivi
description: Sessioni di cassa, vendita al banco, ricevute di cortesia, chiusura con riepilogo Z e corrispettivi.
sidebar:
  order: 10
---

## Cassa

*Cassa* gestisce la sessione di cassa di una postazione:

1. **Apertura** con il fondo cassa iniziale.
2. **Vendita al banco**: articoli, cliente facoltativo, metodo di pagamento e ricevuta.
3. **Movimenti**: versamenti e prelievi, che entrano nel conteggio di chiusura.
4. **Chiusura**: conti l'incassato per metodo; la differenza con l'atteso viene registrata e il **riepilogo Z** resta
   consultabile.

Gli incassi in contanti dell'ormeggiatore, dei transiti alla partenza e dei noleggi passano dalla stessa cassa.

## Ricevute

La ricevuta di cortesia si stampa in 80 o 58 mm o in A4 e riporta **"Non valido ai fini fiscali"**. Dall'app desktop
può uscire su una stampante di ricevute collegata al PC ([App desktop](/gestore/app-desktop/#periferiche)).

## Corrispettivi

Porto **non è un registratore telematico**. In *Corrispettivi* trovi i totali per giorno o mese, aliquota e metodo e i
documenti commerciali. Il comportamento dipende dal modulo fiscale scelto in *Impostazioni addebiti*:

- **nessuno**: il documento commerciale lo emette il gestore con il proprio registratore;
- **esportazione**: Porto produce il riepilogo giornaliero per aliquota e metodo di pagamento da consegnare al
  commercialista;
- **ponte dell'app desktop**: un registratore collegato al PC dell'ufficio può ricevere i documenti; Porto non include
  driver per modelli reali di registratori, che sono estensioni.

Chi chiede la fattura riceve invece un addebito, che finisce nell'esportazione "da fatturare"
([Addebiti e incassi](/gestore/addebiti-e-incassi/)).
