## 24. Sensori di occupazione in tempo reale

### 24.1 Obiettivo

Sapere in tempo reale quali posti sono **fisicamente** occupati, confrontarlo con l'occupazione contrattuale e
agire: rivendere posti di barche fuori, intercettare arrivi non registrati, verificare gli arrivi dei transiti. Il
sistema è **indipendente dall'hardware**: qualsiasi sensore (ultrasuoni, radar, magnetometro, telecamera con
riconoscimento a bordo) che invii lo stato tramite il protocollo documentato.

### 24.2 Dispositivi

- `sensor_devices`: identificativo, tipo, modello, risorsa associata (posto, segmento di area dinamica con
  `from_cm`/`to_cm`, colonnina), posizione sulla mappa, chiave del dispositivo (segreto mostrato una volta, salvato
  come hash), stato (batteria, segnale, ultimo contatto, firmware).
- Gateway (LoRaWAN, NB-IoT, bridge MQTT) gestiti all'esterno; Porto riceve gli eventi già decodificati.

### 24.3 Ingestione

- `POST /api/ingest/v1/readings`: lotti di letture `{device_id, observed_at, state: occupied|free|unknown, distance_cm?,
  battery_pct?, rssi?}` con firma HMAC per dispositivo o chiave del gateway; idempotenza su `(device_id,
  observed_at)`; rate limit per chiave.
- `ext.SensorSource` per sorgenti che si interrogano (API di un fornitore, broker MQTT): un job le legge e passa le
  letture allo stesso servizio.
- **Stato del posto** da una macchina a stati con isteresi (`occupied` dopo N letture concordi o T minuti, default 2
  letture o 5 minuti) per evitare falsi passaggi dovuti al moto ondoso.
- Conservazione: letture grezze 30 giorni (configurabile), poi solo le transizioni di stato (`sensor_state_changes`).

### 24.4 Riconciliazione e azioni

Il servizio `sensor` confronta lo stato fisico con l'occupazione contrattuale e la presenza:

| Situazione | Azione proposta |
|---|---|
| Occupato senza allocazione per la notte | Avviso "posto occupato non previsto" all'ormeggiatore, con la mappa |
| Transito atteso oggi e posto che diventa occupato | Proposta di registrare l'arrivo |
| Contratto annuale, posto libero oltre N ore | Presenza `out`; dopo M giorni proposta di rilascio se `release_policy` lo consente (10.6) |
| Posto rilasciato e rivenduto, che diventa occupato prima dell'arrivo del transito | Avviso di rientro anticipato |
| Sensore senza letture oltre T, batteria sotto soglia | Intervento di manutenzione (20.2) |

Nessuna di queste azioni modifica contratti o occupazioni senza una conferma; l'unico effetto automatico è sullo
**stato di presenza** (se il marina lo abilita) e sugli avvisi.

### 24.5 Visualizzazione e pubblicazione

- Contesto *Sensori* della mappa (9.2), aggiornato via SSE con eventi `sensor.state_changed` privi di dati personali.
- Storico di occupazione fisica per posto; report di discordanza tra occupazione fisica e contrattuale.
- **Occupazione aggregata pubblica** facoltativa (posti liberi per categoria in questo momento) per la scheda
  pubblica del porto, senza mai indicare quali posti né quali barche.
