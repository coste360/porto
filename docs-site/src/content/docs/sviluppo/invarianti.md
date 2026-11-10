---
title: Invarianti
description: Le regole di dominio che Porto garantisce sempre, ciascuna verificata da test.
sidebar:
  order: 3
---

Le invarianti sono le regole che devono restare vere in ogni stato del sistema. Sono elencate, una per una, con il
test che le verifica, in [docs/invariants.md](https://github.com/coste360/porto/blob/main/docs/invariants.md) (circa
cento voci, divise per area). Una modifica che ne tocca una deve mantenerla e lasciarla verificata.

## Le principali

- **Nessuna doppia occupazione.** Posti, celle delle aree dinamiche, risorse a terra, unità a noleggio, skipper e
  mezzi di sollevamento sono protetti da **vincoli unici del database**, non da controlli applicativi. I test di
  concorrenza lanciano almeno 100 tentativi paralleli, con `-race`, su SQLite e PostgreSQL.
- **La notte è l'unità di occupazione**: un soggiorno occupa le notti dall'arrivo alla partenza esclusa.
- **Importi esatti.** Centesimi interi ovunque; un piano rate somma esattamente al totale; il riparto delle spese
  comuni somma esattamente al preventivo (metodo del resto maggiore).
- **Prezzi riproducibili.** Un listino attivato non si modifica più e ogni prezzo resta ricalcolabile dal suo
  snapshot; la matrice del listino coincide con il simulatore.
- **Stati dei contratti in un solo punto.** Ogni cambio di stato di contratti e transiti passa da un unico servizio e
  scrive audit ed evento nella stessa transazione.
- **Conferma dei transiti solo dal pagamento.** Un transito online si conferma solo dal webhook firmato del
  pagamento o dal driver manuale, mai dal ritorno del browser.
- **Addebiti esportati immutabili.** Ogni addebito finisce nell'esportazione "da fatturare" una volta sola; dopo, le
  correzioni sono addebiti di rettifica collegati.
- **Abbinamenti coerenti.** La somma degli abbinamenti di un incasso non supera né l'incasso né il residuo
  dell'addebito; le cauzioni non sono ricavi finché non vengono trattenute.
- **Isolamento dei marine.** Ogni query operativa è filtrata per marina; una risorsa di un altro marina risponde 404.
- **Nessun dato personale** in log, eventi, messaggi SSE e webhook.
- **I sensori propongono, non decidono**: nessuna lettura cambia un contratto o un'occupazione senza la conferma di
  un operatore.
- **Le ipotesi non toccano lo stato reale**: un'ipotesi di rimessaggio si applica tutta o niente, ricontrollata sullo
  stato attuale.
- **Il cruscotto coincide con i report**: il valore di ogni riquadro è la somma dei record del suo drill-down.
