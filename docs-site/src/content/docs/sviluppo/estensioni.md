---
title: Estensioni
description: I punti di estensione di pkg/ext - pagamenti, fatturazione, firma, canali, sensori, notifiche, eventi.
sidebar:
  order: 4
---

Quello che varia da un porto all'altro e dipende da fornitori esterni sta dietro interfacce Go pubbliche, nel package
[`pkg/ext`](https://github.com/coste360/porto/tree/main/pkg/ext). Il package non importa nulla di `internal/`:
un'estensione si scrive contro queste interfacce e si registra nel binario, passando le implementazioni in
`ext.Extensions` dove `cmd/porto` crea l'applicazione.

| Interfaccia | Per cosa | Nel core |
|---|---|---|
| `PaymentProvider` | gateway e metodi di pagamento | driver `manual` (sempre attivo) e `stripe` |
| `FiscalDriver` | registratori telematici e documento commerciale | `none`, `export` e il ponte dell'app desktop |
| `ChargeExporter` | invio degli addebiti a un software di fatturazione e ritorno di numero e data della fattura | nessuno |
| `SignatureProvider` | firma elettronica avanzata o qualificata | nessuno (firma su carta, OTP e su schermo sono nel core) |
| `BookingChannel` | portali di prenotazione degli ormeggi | nessuno |
| `SensorSource` | sorgenti di letture da interrogare (API di un fornitore, broker MQTT) | nessuna (i sensori possono anche inviare direttamente, [protocollo](/api/ingestione-sensori/)) |
| `Notifier` | canali di notifica aggiuntivi (SMS, WhatsApp) | nessuno (l'email è nel core) |
| `EventSink` | ricevere ogni evento dell'outbox, almeno una volta | webhook e SSE |
| `TenantResolver` | domini personalizzati per le pagine pubbliche | — |
| `AuthProvider`, `BotProtection` | accesso con provider esterni, protezione dei moduli pubblici | — |
| `HTTPRoutes` | rotte aggiuntive sul router | — |

## Regole per le estensioni

- **Nessun dato personale** nei log, nei metadati inviati ai provider e negli eventi: solo id e valori enumerati.
- **HTTP in uscita** solo con il client che Porto passa al driver: `https`, niente indirizzi privati o di metadati,
  redirect ricontrollati.
- **Importi** in centesimi interi con la valuta ISO 4217 esplicita; nessun numero decimale.
- Le chiamate ai provider avvengono **fuori dalle transazioni** del database; l'esito arriva di norma da un webhook
  firmato.
- **Gli eventi possono arrivare più volte**: `EventSink.Handle` deve essere idempotente.
- Un canale di prenotazione non può creare sovrapposizioni: le prenotazioni in ingresso passano dallo stesso servizio
  dei transiti e occupano un posto concreto.

## Prima di scrivere un driver

Apri una issue *nuovo driver* sul repository per concordare l'interfaccia e capire se il driver appartiene al core o a
un'estensione separata. Vedi [Contribuire](/sviluppo/contribuire/) e, per i pagamenti,
[Driver di pagamento](/sviluppo/driver-di-pagamento/).
