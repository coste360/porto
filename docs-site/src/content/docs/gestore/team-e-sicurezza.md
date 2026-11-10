---
title: Team e sicurezza
description: Utenti e ruoli, autenticazione a due fattori, chiavi API, dispositivi, webhook e registro attività.
sidebar:
  order: 21
---

## Team

*Team* elenca le persone che lavorano sul porto con il loro ruolo; da qui inviti nuovi utenti per email, cambi il ruolo
o rimuovi qualcuno. Un utente può far parte dell'intera organizzazione (vale per tutti i suoi marine) o di un singolo
marina, e può avere ruoli diversi in marine diversi. L'ultimo titolare non si può rimuovere.

I ruoli sono descritti nella [Panoramica](/gestore/#ruoli). I permessi sono verificati dal server su ogni operazione,
non solo nascondendo i menu.

## Accesso

- Password di almeno 12 caratteri, conservate con Argon2id.
- **Autenticazione a due fattori** (TOTP) da *Il mio account*, con codici di recupero; può diventare obbligatoria per
  titolari e amministrazione (`PORTO_REQUIRE_TOTP_FOR_OWNERS`).
- Reimpostazione della password via email con un link monouso.
- Limite di 5 tentativi sbagliati per account in 15 minuti.

## Chiavi API

*Chiavi API* crea le chiavi per collegare altri programmi: si scelgono gli scope, la chiave è mostrata una sola volta e
si revoca in ogni momento ([Autenticazione](/api/autenticazione/)).

## Dispositivi

*Dispositivi* abbina telefoni, tablet e postazioni con un codice o un QR e permette di revocarli
([Telefono e offline](/gestore/telefono-e-offline/)).

## Webhook

*Webhook* invia gli eventi del porto ad altri sistemi, firmati, con registro delle consegne e reinvio
([Eventi e webhook](/api/eventi-e-webhook/)).

## Registro attività

*Registro attività* mostra chi ha fatto cosa e quando: ogni scrittura dal pannello, dall'API o dai processi automatici
lascia una riga.

## Dati e privacy

I dati restano sul computer o sul server che scegli: nessuno, nemmeno Coste360, vi accede. Documenti, IBAN e segreti dei
provider sono cifrati; log, eventi e webhook non contengono dati personali. Il titolare del trattamento è il gestore
([Clienti e imbarcazioni](/gestore/clienti-e-imbarcazioni/#doppioni-e-gdpr)).
