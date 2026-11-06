## 20. Manutenzioni e risorse a terra

### 20.1 Piani di manutenzione degli ormeggi

- **Elementi manutenibili:** posti (corpo morto, catenaria, trappa, cime), pontili e moduli galleggianti, finger,
  boe, colonnine, passerelle, mezzi di sollevamento, risorse a terra.
- **Piani** (`maintenance_plans`): tipo di intervento (ispezione subacquea, sostituzione trappe, verifica catenarie,
  test differenziali delle colonnine), periodicità (ogni N mesi, ogni N cicli o a data fissa), anticipo dell'avviso,
  responsabile o fornitore, checklist.
- Il job `maintenance.schedule` genera gli interventi in scadenza; gli avvisi compaiono nel pannello, nel contesto
  *Manutenzione* della mappa e nella lista di lavoro sul telefono.

### 20.2 Interventi

- `maintenance_tasks`: elemento, piano di origine o segnalazione (anche dal telefono con foto e posizione sulla mappa),
  priorità, stato (`open`, `scheduled`, `in_progress`, `done`, `cancelled`), esito, costi (ore interne, costi
  esterni con importo e riferimento del fornitore), foto e documenti.
- Un intervento che richiede il posto libero crea un'occupazione `maintenance` per le notti previste (10.4); se ci
  sono barche assegnate il sistema propone gli spostamenti (10.5) prima di confermarlo.
- Stato **fuori servizio** di un elemento: il posto non è vendibile e la mappa lo marca.

### 20.3 Risorse a terra

Posti auto, box, magazzini, armadietti, invasi e carrelli (7.6) si gestiscono con la stessa logica dei posti:
occupazione per notte, contratti propri o righe di un contratto di ormeggio, listino, mappa con contesti, ricerca
delle disponibilità. Un posto auto incluso nel contratto annuale è una riga del contratto con la sua allocazione.

### 20.4 Utenze e letture

- Colonnine con contatori (7.7): letture periodiche dal telefono (foto del contatore facoltativa) o da contatori
  intelligenti tramite `ext.SensorSource` (24.4).
- Consumo = lettura − lettura precedente per presa e periodo, attribuito al contratto attivo sul posto collegato
  (ripartito per notti se nel periodo cambia l'occupante); addebito con l'articolo `metered` del listino, insieme
  alla rata successiva o a fine soggiorno per i transiti.
- Letture anomale (consumo negativo, oltre una soglia) sono segnalate prima dell'addebito.
