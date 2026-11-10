---
title: Domande frequenti
description: Costi, licenza, fatturazione, prenotazione online, uso senza internet, più marine e protezione dei dati.
sidebar:
  order: 13
---

## Esiste un gestionale per porti turistici gratuito?

Sì: Porto è un gestionale portuale gratuito, con codice sorgente disponibile, per porti turistici, marine, approdi e
cantieri nautici. Lo installi sul tuo server o sul PC dell'ufficio, senza canoni né licenze a pagamento, e comprende
mappa del porto, contratti di ormeggio e transiti, prenotazione online dei posti barca, addebiti e incassi,
rimessaggio, cantiere, noleggio e report.

## Porto è davvero gratuito?

Sì, per chi gestisce la propria struttura. Per i porti, gli approdi, i cantieri o i rimessaggi che possiedi o gestisci
direttamente non ci sono canoni, versioni a pagamento o limiti sul numero di posti barca, contratti o utenti: l'unico
costo è il computer o il server su cui gira. Una licenza commerciale serve solo a chi vuole offrire Porto come
servizio a terzi ([Licenza e marchio](/sviluppo/licenza/)).

## Ho solo un cantiere o un rimessaggio, senza posti in acqua: posso usarlo?

Sì. Cantieri e rimessaggi rientrano nell'uso gratuito come i porti, e puoi usare solo le parti che ti servono: mappa
del piazzale e del capannone, contratti di rimessaggio con l'ottimizzatore, preventivi e commesse di cantiere, mezzi di
sollevamento, addebiti e incassi.

## Sono un'agenzia, un tecnico o una software house: posso installarlo per i miei clienti?

Non senza una licenza commerciale. La licenza gratuita copre solo le strutture che il gestore possiede o gestisce
direttamente: installare, configurare, personalizzare, ospitare (hosting o SaaS), mantenere o assistere Porto per
altri porti a pagamento, o rivenderlo, richiede una licenza commerciale di Coste360 SRL (info@coste360.it). Il gestore
invece può installarlo da sé, con i propri dipendenti o su un server che ha noleggiato.

Chi realizza il sito del porto può inserirci il widget o il link alla pagina di prenotazione senza licenza, purché non
installi, ospiti, configuri o assista Porto.

## Siamo un consorzio o un'associazione di porti: possiamo usarlo per i soci?

Ogni porto socio può installare e usare Porto gratis per sé. Un consorzio o un'associazione che non controlla i porti
soci non li gestisce direttamente: per installare, ospitare o assistere Porto per i soci serve una licenza commerciale
di Coste360 SRL.

## Perché la licenza è la BSL e non una licenza open source?

Perché Porto deve restare gratuito per i gestori senza che altri lo rivendano come servizio a pagamento agli stessi
porti. La Business Source License 1.1 lascia il codice leggibile, modificabile e aperto ai contributi, ma riserva a
Coste360 l'offerta commerciale a terzi. Ogni versione passa poi alla licenza AGPL-3.0 o successiva tre anni dopo il
suo primo rilascio pubblico. I dettagli sono in [Licenza e marchio](/sviluppo/licenza/).

## Porto emette le fatture elettroniche?

No. Porto calcola e tiene gli addebiti (rate, transiti, servizi, consumi, commesse, noleggi, oneri) e li esporta "da
fatturare" in CSV, Excel o PDF per il software di fatturazione che usi già; il riscontro collega poi le fatture emesse
agli addebiti. Porto non è nemmeno un registratore telematico. Vedi [Addebiti e incassi](/gestore/addebiti-e-incassi/)
e [Cassa e corrispettivi](/gestore/cassa-e-corrispettivi/).

## Ci sono commissioni sui transiti prenotati online?

No. I pagamenti online passano dal tuo account Stripe e l'incasso arriva a te: paghi solo le commissioni del circuito
di pagamento, nessuna percentuale alla piattaforma. Puoi anche non attivare i pagamenti online e confermare i transiti
dal banco, con bonifico o in cassa.

## Posso usarlo con il mio sito WordPress?

Sì. Il widget di prenotazione si inserisce in un blocco "HTML personalizzato" di WordPress, o in Wix, Joomla,
Squarespace e qualsiasi altro sito, con uno script di una riga o un iframe; in alternativa basta un link alla pagina di
prenotazione o alla scheda del porto. Vedi [Prenotazione online](/gestore/prenotazione-online/).

## Funziona senza internet?

Sì. Con l'[app desktop](/gestore/app-desktop/) server e archivio stanno sul PC dell'ufficio; gli altri PC e i telefoni
degli ormeggiatori si collegano sulla Wi-Fi del porto. Il pannello sul telefono tiene in coda arrivi, partenze, foto,
letture e incassi registrati senza rete e li invia al ritorno della connessione, senza doppioni
([Telefono e offline](/gestore/telefono-e-offline/)). Internet, e quindi un server raggiungibile, serve solo per la
prenotazione online, i pagamenti online e la firma a distanza.

## Porto usa l'intelligenza artificiale?

No. Porto non contiene funzioni di IA e non invia dati a servizi di IA: i dati dei documenti li inserisce l'operatore,
con il file allegato e controlli deterministici
([ADR 0008](https://github.com/coste360/porto/blob/main/docs/adr/0008-nessuna-intelligenza-artificiale.md)). Non c'è
nemmeno telemetria.

## Posso gestire più marine?

Sì. Un'unica installazione gestisce più marine, ognuna con mappe, listini, contratti, utenti e pagina di prenotazione
propri. Con molte strutture o oltre i 300 posti è consigliato [PostgreSQL](/installazione/database/).

## Quali tipi di ormeggio e di spazi supporta?

Posti fissi su pontili e banchine, con finger o a corpo morto, posti in testata, campi boe, aree dinamiche dove le
barche si affiancano secondo la lunghezza, piazzali e capannoni a celle per il rimessaggio, parcheggi e box. Le barche
occupano il loro ingombro reale e Porto controlla che stiano nel posto. L'ormeggio in seconda fila non è gestito
([Limiti noti](/installazione/#limiti-noti)).

## I dati dei miei clienti sono al sicuro e conformi al GDPR?

I dati restano sul computer o sul server che scegli tu: nessuno, nemmeno Coste360, vi accede. Porto raccoglie solo i
dati necessari, cifra documenti e IBAN, versiona i consensi, consente l'esportazione e l'anonimizzazione dei dati del
cliente e non scrive email, telefoni o documenti in chiaro nei log. Il titolare del trattamento resta il gestore.
