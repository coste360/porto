## 5. Modello di dominio e glossario

### 5.1 Glossario

| Termine | Codice | Significato |
|---|---|---|
| **Organizzazione** | `organization` | Società di gestione. Può avere più marine e cantieri. |
| **Marina** | `marina` | Un porto turistico, approdo o darsena. Ha fuso orario, mappe, listini, casse. Tutte le query operative sono filtrate per `marina_id`. |
| **Mappa** | `map` | Rappresentazione in scala di un'area: specchio acqueo, piazzale, capannone, parcheggio, box. Un marina ha più mappe. |
| **Banchina** | `quay` | Opera fissa a terra lungo cui si ormeggia (polilinea). |
| **Pontile** | `pier` | Struttura fissa o galleggiante che si protende nello specchio acqueo, con posti su uno o due lati e in testata. |
| **Finger** | `finger` | Passerella galleggiante corta tra due posti, per l'ormeggio di fianco. |
| **Gruppo** | `berth_group` | Raggruppamento logico di posti (pontile B, "posti transito", "lato nord") usato da listini, report, quote online e filtri. |
| **Posto barca** | `berth` | Spazio d'ormeggio a geometria fissa con dimensioni massime (lunghezza, larghezza, fondale) e tipo di ormeggio. |
| **Campo boe** | `buoy_field` | Area con boe d'ormeggio; ogni boa è un posto con raggio di rotazione. |
| **Area dinamica** | `dynamic_area` | Spazio senza posti predefiniti, diviso in celle: tratto lineare di banchina o pontile (*pontile dinamico*), oppure superficie 2D (piazzale, capannone). Le barche si collocano in scala dove c'è spazio. |
| **Cella** | `cell` | Unità minima di occupazione di un'area dinamica (default 50 cm lineari o 100×100 cm). |
| **Risorsa a terra** | `land_resource` | Elemento discreto a terra: posto auto, box, magazzino, armadietto, invaso. |
| **Imbarcazione** | `vessel` | Barca con dimensioni, tipo, documenti, assicurazione, proprietari. |
| **Cliente** | `customer` | Persona o società: armatore, conduttore, titolare di diritti, cliente del cantiere o del noleggio. |
| **Notte** | `night` | Unità di occupazione: la notte che inizia in quella data. Arrivo il 10 e partenza il 12 = notti 10 e 11. |
| **Occupazione** | `occupancy` | Riga che occupa una risorsa (posto, cella, risorsa a terra) per una notte. È ciò che impedisce le doppie assegnazioni. |
| **Preventivo** | `quote` | Offerta con prezzo calcolato, validità, eventuale opzione sul posto. |
| **Contratto** | `contract` | Accordo con il cliente: ormeggio (annuale, stagionale, mensile, a periodo), transito, rimessaggio, charter. |
| **Transito** | `contract kind=transit` | Soggiorno breve, a notti, spesso prenotato online. |
| **Diritto d'uso** | `berth_right` | Diritto pluriennale su un posto (multiproprietà, socio, acquisto fino alla scadenza della concessione). |
| **Cessione** | `right_transfer` | Passaggio di un diritto d'uso da un titolare a un altro. |
| **Mandato di gestione** | `management_mandate` | Il titolare affida il posto al marina per affittarlo quando non lo usa, con ripartizione dei ricavi. |
| **Oneri di gestione** | `management_fee` | Somma annua dovuta dal titolare di un diritto per la gestione del porto. |
| **Spese condominiali** | `common_charges` | Costi comuni ripartiti tra i titolari secondo tabelle millesimali. |
| **Rata** | `installment` | Quota di pagamento di un contratto con scadenza; alla data di emissione genera un addebito. |
| **Presenza** | `presence` | Stato fisico della barca: in porto, fuori, a terra, in cantiere. Distinto dall'occupazione contrattuale. |
| **Rilascio** | `release` | Notti in cui il conduttore libera il posto, rivendibili ai transiti. |
| **Articolo** | `item` | Prodotto o servizio vendibile con codice, aliquota IVA, classificazione, centro di ricavo. |
| **Listino** | `price_list` | Insieme di regole di prezzo per un periodo di validità. |
| **Addebito** | `charge` | Importo dovuto dal cliente, con righe, IVA, competenza e scadenza. Si incassa in Porto e si esporta al software di fatturazione esterno, che emette la fattura. |
| **Commessa** | `work_order` | Lavorazione di cantiere su un'imbarcazione. |
| **Ipotesi** | `scenario` | Disposizione alternativa delle barche in una rimessa, calcolata e confrontata prima di applicarla. |
| **Contesto** | `map_lens` | Criterio con cui la mappa colora e marca gli elementi (contratto, presenza, pagamenti, ...). |

### 5.2 Tipi di risorsa occupabile

Ogni risorsa occupabile appartiene a una di tre famiglie, gestite dallo stesso motore (sezione 10):

| Famiglia | Esempi | Unità di occupazione |
|---|---|---|
| `berth` | Posto barca, boa | il posto intero, per notte |
| `dynamic_area` | Pontile dinamico, banchina transiti, piazzale, capannone | intervallo di celle, per notte |
| `land_resource` | Posto auto, box, magazzino, invaso | la risorsa intera, per notte |

Il noleggio (sezione 19) e le gru di cantiere (sezione 18.7) usano **fasce orarie** invece delle notti, sullo
stesso principio.
