---
title: App desktop
description: Porto sul PC dell'ufficio senza internet - primo avvio, rete locale, backup, chiave segreta, modalità remota, chiosco e periferiche.
sidebar:
  order: 20
---

L'app desktop per **Windows, macOS e Linux** contiene un server Porto completo con il suo archivio (SQLite) sul PC
dell'ufficio. Lavora **senza internet** e senza alcun servizio di Coste360. Le impostazioni sono in *App desktop*.

## Installazione e primo avvio

Scarica il pacchetto per il tuo sistema ([Scegli come installare](/installazione/scegli-come-installare/#app-desktop)) e
avvialo. Al primo avvio si apre la pagina di benvenuto: crei il titolare e l'organizzazione, poi il marina. In
alternativa la stessa pagina ripristina un backup, oppure collega l'app a un Porto online (modalità remota).

I dati stanno nella cartella dell'utente:

| Sistema | Cartella |
|---|---|
| macOS | `~/Library/Application Support/Porto` |
| Windows | `%LOCALAPPDATA%\Porto` |
| Linux | `~/.local/share/porto` |

## Chiave segreta

La chiave che cifra documenti e file sta nel portachiavi del sistema. Il titolare può mostrarla in *App desktop*
(chiede di nuovo la password): **conservala in un posto sicuro**, serve per ripristinare i backup su un altro computer o
per passare a un server online.

## Backup

L'app fa un backup al giorno e uno a ogni chiusura, oltre a quelli manuali. Ne tiene 14 giornalieri e 5 di chiusura; i
manuali non vengono mai cancellati. Puoi scegliere la cartella (anche un disco esterno). Il ripristino si avvia dal
pannello: prima viene fatto un backup di sicurezza, poi l'app si riavvia con i dati del backup. I backup sono nel
formato di `porto restore` e si possono ripristinare anche su un server.

## Rete locale

È spenta finché il titolare non la attiva. Con la rete locale gli altri PC dell'ufficio e i telefoni sulla Wi-Fi del
porto usano lo stesso archivio, in HTTPS con un certificato generato in locale (porta 8443). I telefoni si abbinano con
il QR in *Dispositivi* e accettano il certificato una volta; il certificato dell'app si può anche installare sul telefono
per avviare il pannello senza rete. I telefoni funzionano finché l'app è aperta.

## Modalità remota

L'app può fare da postazione di un **Porto online** già attivo: i dati restano sul server, l'app custodisce il token del
dispositivo e un **PIN** protegge le sue impostazioni. Se il server non è raggiungibile il pannello si apre comunque e le
operazioni ammesse finiscono nella coda offline.

## Modalità chiosco

Per il banco: la finestra occupa tutto lo schermo, senza menu né barra delle applicazioni. Si esce con
**Cmd/Ctrl+Shift+K** e il PIN (modalità remota) o le credenziali di un titolare.

## Periferiche

In *Periferiche* configuri ciò che è collegato al PC:

- **stampante di ricevute** ESC/POS (80/58 mm) in rete o tramite le code di stampa di macOS e Linux;
- **stampante di etichette** ZPL;
- **lettore QR** USB che scrive come una tastiera;
- **registratore telematico** locale: Porto include l'interfaccia ma nessun driver per modelli reali.

## Aggiornamenti

Una volta al giorno l'app controlla se esiste una nuova versione e lo segnala con un avviso, inviando solo numero di
versione e sistema operativo. Non scarica e non installa mai nulla; il controllo si disattiva dalle impostazioni.

## Limiti

Senza un server raggiungibile da internet non ci sono pagina pubblica di prenotazione, pagamenti online e firma a
distanza. Per averli si passa a un server con un backup e un ripristino. I pacchetti si compilano già, ma la firma e la
notarizzazione richiedono i certificati di Coste360: fino ad allora Windows e macOS mostrano un avviso al primo avvio.
