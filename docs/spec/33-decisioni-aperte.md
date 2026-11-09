## 33. Decisioni aperte

Decisioni da prendere prima della milestone indicata. Ognuna, una volta presa, diventa un ADR.

| # | Decisione | Proposta | Entro |
|---|---|---|---|
| D4 | Software di fatturazione di destinazione | Esportazione Excel/CSV nel core; scegliere i software più usati dai marina clienti (o dai loro commercialisti) per il formato del file e un eventuale primo `ChargeExporter` | M5 |
| D5 | Firma InfoCert | Verificare contratto, API e costi del servizio InfoCert per la firma avanzata/qualificata; nel frattempo `paper` e `otp` | M4 |
| D6 | Valore legale della firma OTP per i contratti di ormeggio | Parere legale; la documentazione la presenta come firma elettronica semplice | M4 |
| D7 | Hardware dei sensori | Protocollo aperto (24.3) e un partner hardware di riferimento per la certificazione | M12 |
| D8 | Comunicazioni obbligatorie (autorità marittima, ospiti a bordo) | Verifica normativa per regione; eventuali estensioni per porto | M8 |
| D9 | Mercati esteri (Svizzera, Croazia, ...) | Prima versione per l'Italia; la valuta per marina è già nel modello; i corrispettivi esteri richiedono driver fiscali dedicati | dopo M12 |
| D10 | App nativa per i diportisti | La PWA della pagina pubblica copre la prenotazione; un'app nativa solo se emerge una richiesta (account del diportista su più porti) | dopo M8 |
| D12 | Ormeggio in seconda fila (affiancamento) | Escluso dalla prima versione; richiederebbe allocazioni dipendenti da un'altra barca | dopo M12 |
| D14 | Dimensione minima di una categoria nell'occupazione aggregata pubblica (24.5) | Con una categoria di uno o due posti il conteggio rivela lo stato di un singolo posto (INV-103). Proposta: soglia minima di posti (per esempio 3) sotto la quale la categoria non si può pubblicare oppure il conteggio è mostrato a fasce; serve una scelta di prodotto sul numero e sul comportamento | M12 |

Decise e tolte dalla tabella dopo l'ultimo ADR:

- **D2 — Licenza (decisa il 2026-11-09):** BSL 1.1 come Arenile, conversione AGPL (AGPL-3.0 o successiva tre anni
  dopo il primo rilascio pubblico di ogni versione); concessione d'uso gratuita per i porti, gli approdi, i cantieri
  e i rimessaggi gestiti direttamente. Testo vincolante in [`LICENSE`](../../LICENSE); l'ADR è da scrivere.
