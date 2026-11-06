## 21. Report e cruscotto statistico

### 21.1 Principi

Come in Arenile (ADR Arenile 0016): report di sola lettura, query in `internal/store/report.go` con righe grezze
filtrate per `marina_id`, aggregazioni e indicatori come **funzioni pure** testate a tabella, CSV generato dalle stesse
righe del JSON. Ogni indicatore del cruscotto ha una definizione scritta qui e un drill-down fino ai record.

### 21.2 Cruscotto del manager

Riquadri con valore del periodo, confronto con lo stesso periodo dell'anno precedente e andamento:
- **Produzione:** valore dei contratti e dei transiti di competenza del periodo (21.3 *competenze*).
- **Addebitato** e **incassato**; sospesi aperti e scaduti.
- **Occupazione** e **efficienza allocativa** (definizioni sotto), per marina, gruppo, pontile, categoria.
- **Ricavo per metro lineare di posto** e **ricavo medio per notte** dei transiti.
- **Clientela:** nuovi clienti, rinnovi, tasso di rinnovo, nazionalità, canale di acquisizione.
- **Imbarcazioni ospitate:** per tipo, fascia di lunghezza, bandiera; lunghezza media.
- **Cantiere:** commesse aperte, ore lavorate, margine; **noleggio:** tasso di utilizzo della flotta, ricavi.
Ogni riquadro si apre nel report corrispondente con lo stesso filtro.

### 21.3 Report e definizioni

| Report | Definizione |
|---|---|
| **Occupazione degli ormeggi** | Per notte e risorsa: `occupazione = notti-posto occupate / notti-posto disponibili` (escluse le notti fuori servizio e le chiusure). Occupate = righe `contract`, `transit`, `owner`; i rilasci non rivenduti contano come libere nella variante "fisica" e occupate in quella "contrattuale". Per le aree dinamiche: metri-notte occupati / metri-notte disponibili. |
| **Efficienza occupazionale** | `Σ superficie barca × notti / Σ superficie posto × notti` sulle notti occupate (superficie barca = `loa × beam`; aree dinamiche: lunghezza occupata con margini / lunghezza assegnata). |
| **Spreco allocativo** | Per allocazione: `1 − superficie barca / superficie posto`; report delle allocazioni oltre la soglia (default 40 %) con il **ricavo potenziale perso** = prezzo di listino del posto per la sua categoria − prezzo applicato, e suggerimenti di scambio tra barche che ridurrebbero lo spreco senza conflitti (calcolati con `fit` e la ricerca 10.7). |
| **Domanda persa** | Richieste non evase (13.2) per motivo, periodo, fascia di lunghezza e canale, con il valore stimato al prezzo di listino: dice quali dimensioni e quali settimane mancano, utile per listini, quote online e riconfigurazione dei pontili. |
| **Clientela** | Clienti per categoria, nazionalità, anzianità, valore (produzione per cliente), morosità, rinnovi. |
| **Competenze, ratei e risconti** | Funzione pura `accrual.Allocate(righe, periodo)`: ogni riga di addebito o di contratto con `service_from/to` è ripartita per giorni di competenza. *Competenza* del periodo = quota dei giorni nel periodo. *Risconto* = importo addebitato con competenza successiva alla data di chiusura. *Rateo* = competenza maturata entro la data e non ancora addebitata. Sono valori gestionali per il commercialista, che li riporta in contabilità. Il resto dell'arrotondamento è assegnato all'ultimo giorno, così la somma delle quote è uguale all'importo (INV-60). |
| **Addebiti e incassi dettagliati** | Addebiti e righe per periodo, cliente, articolo, origine, stato di pagamento e di fatturazione (da fatturare, esportato, fatturato), con imponibile, IVA e totale; incassi per metodo e conto del provider. Due basi di data come Arenile: competenza e incasso. |
| **Classificazione dei ricavi** | Imponibile per `revenue_class`, per mese, con percentuali. |
| **Comparazione delle statistiche** | Qualsiasi indicatore confrontato tra due periodi o anni, allineando per data o per giorno della settimana (come `align` di Arenile). |
| **Scadenzario e crediti** | Rate e documenti aperti per età; solleciti inviati. |
| **Presenze** | Arrivi, partenze, notti di assenza dei contratti annuali, rilasci e rivendite, ricavi dei rilasci. |
| **Diritti d'uso e condominio** | Titolari, oneri emessi e incassati, ripartizioni e conguagli. |
| **Cantiere** | Ore per dipendente e commessa, carico, margini per commessa e per tipo di lavorazione. |
| **Noleggio** | Utilizzo per unità (ore noleggiate / ore disponibili), ricavi, danni e cauzioni trattenute. |
| **Cassa e corrispettivi** | Sessioni di cassa, differenze, corrispettivi per aliquota e metodo (come Arenile). |

Esportazioni CSV (italiano: separatore `;`, virgola decimale) ed Excel (`internal/xlsx` di Arenile).
