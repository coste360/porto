## 14. Cessioni, oneri di gestione e spese condominiali

In molti porti turistici una parte dei posti è oggetto di **diritti d'uso pluriennali** (acquisto del diritto fino
alla scadenza della concessione demaniale, quote di società o consorzi, multiproprietà). Il marina gestisce i
titolari, i passaggi di titolarità, gli oneri annui e la ripartizione delle spese comuni.

### 14.1 Diritti d'uso

- `berth_rights`: posto, titolare (uno o più clienti con quota in millesimi, la somma è 1000), tipo
  (`ownership_share`, `long_term_use`, `membership`), decorrenza, scadenza (di default la scadenza della concessione
  demaniale del marina, 14.6), titolo (atto, numero, notaio, data), documenti.
- Il diritto occupa il posto con `kind = owner` per tutto il periodo (10.4). Un posto ha al più un diritto attivo
  per notte (vincolo garantito dalle occupazioni).
- Il titolare può ormeggiare una barca propria o di terzi autorizzati (`authorized_vessels`), con `fit` normale.

### 14.2 Contratti di cessione

- **Cessione** (`right_transfers`): dal titolare uscente al subentrante, con data di efficacia, corrispettivo
  dichiarato (informativo), diritti di subentro dovuti al marina (articolo del listino), verifica dei requisiti
  (oneri e spese pagati: blocco o avviso secondo la policy), documenti (atto, nulla osta).
- In una transazione: chiusura del diritto del cedente alla data, nuovo diritto del cessionario, trasferimento delle
  occupazioni future `owner`, rate future degli oneri riassegnate o conguagliate pro rata, audit ed evento
  `right.transferred`.
- Catena storica dei titolari per posto, stampabile.

### 14.3 Mandato di gestione

- Il titolare affida al marina il posto per periodi in cui non lo usa (`management_mandates`: periodo, ripartizione
  dei ricavi in %, eventuale minimo garantito, canali consentiti).
- Nel periodo del mandato le notti `owner` sono **rilasciate** automaticamente e il posto diventa vendibile ai
  transiti o a contratti di periodo.
- I ricavi delle notti rivendute sono attribuiti al titolare secondo la ripartizione: rendiconto periodico per
  titolare. La quota del titolare diventa un credito da compensare con gli oneri, oppure un importo da
  bonificare, secondo la configurazione; il rendiconto è esportabile per il commercialista.

### 14.4 Oneri di gestione

- Calcolo annuo per ogni diritto attivo con una **tabella degli oneri** (`management_fee_tables`): importo per
  categoria di posto, per m² del posto (`per_m2_berth`) o per fascia di lunghezza, con adeguamento ISTAT
  facoltativo; pro rata per i diritti che iniziano o finiscono nell'anno; ripartizione tra i cotitolari secondo le
  quote.
- Piano rate (es. due rate semestrali), addebiti e solleciti come i contratti (13.7, 16.4).
- Simulazione prima dell'emissione: totale per categoria, differenze rispetto all'anno precedente, titolari esclusi
  (con motivazione).

### 14.5 Spese condominiali

- **Tabelle millesimali** (`millesimal_tables`): più tabelle per marina (generale, pontile B, servizi a terra), ogni
  posto con i suoi millesimi; la somma della tabella deve essere 1000 per essere usata (INV-41).
- **Preventivo annuo** delle spese comuni per voci (manutenzione pontili, pulizia specchio acqueo, guardiania,
  illuminazione, assicurazione delle parti comuni), ogni voce assegnata a una tabella.
- **Ripartizione:** per ogni titolare l'importo = Σ voce × millesimi / 1000, con arrotondamento al centesimo e resto
  distribuito con il metodo del resto maggiore, così la somma ripartita è esattamente il totale (INV-42).
- Rate di acconto sul preventivo; a fine anno **consuntivo** con i costi effettivi per voce, inseriti dal gestore o
  importati da CSV, e **conguaglio** per titolare (addebito oppure credito).
- Rendiconto stampabile per titolare e per voce.

### 14.6 Concessione demaniale del porto

Scheda compilata a mano dal gestore (`concessions`): ente concedente, numero e tipo dell'atto, durata e scadenza,
canone annuo e sue rate, superfici, prescrizioni, documenti, adempimenti con scadenza (canone, cauzione, polizze,
comunicazioni periodiche). La scadenza è il limite predefinito dei diritti d'uso (14.1) e dei contratti
pluriennali; gli adempimenti entrano nel motore delle scadenze (11.5). Nessun collegamento a sistemi esterni.
