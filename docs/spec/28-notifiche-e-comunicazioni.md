## 28. Notifiche e comunicazioni

### 28.1 Notifiche transazionali

Dall'outbox alle email come Arenile (spec Arenile 19, ADR Arenile 0022: deduplica per evento e modello, consegna con
claim esclusivo e backoff, trasporti SMTP, file, log; modelli Markdown in it, en, de, fr). Modelli predefiniti:
preventivo, conferma del transito con istruzioni di arrivo, promemoria di arrivo, richiesta di firma, contratto
attivato, proposta di rinnovo, richiesta di pagamento della rata con link, ricevuta dell'incasso, avviso di scadenza
della rata, solleciti per livello, scadenza di documenti e assicurazione, rilascio rivenduto (credito maturato),
rendiconto del mandato, commessa pronta per la consegna, noleggio confermato.

### 28.2 Comunicazioni ai clienti

- **Invii massivi** dal pannello: destinatari da un filtro (gruppo, pontile, tipo di contratto, titolari, barche
  presenti in un periodo, clienti con scadenze) o da una selezione sulla mappa; modello o testo libero con campi di
  unione; allegati; anteprima per destinatario; programmazione.
- Rispetto dei consensi: le comunicazioni di servizio (avvisi operativi, chiusure del porto, allerte meteo) vanno a
  tutti i contratti attivi; quelle promozionali solo a chi ha il consenso marketing.
- Registro degli invii per cliente (`message_log`) con stato di consegna; disiscrizione con un clic per le
  promozionali.
- Canali aggiuntivi (SMS, WhatsApp) tramite `ext.Notifier`.

### 28.3 Avvisi interni

Lista di lavoro dell'ufficio e dell'ormeggiatore con scadenze, solleciti da approvare, addebiti da esportare,
manutenzioni in scadenza, anomalie dei sensori, arrivi non previsti; ogni avviso ha un responsabile e uno stato.
