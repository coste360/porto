---
title: Sensori di occupazione
description: Stato fisico dei posti dai sensori, riconciliazione con i contratti, avvisi e proposte da confermare.
sidebar:
  order: 17
---

I sensori dicono quali posti sono **fisicamente** occupati. Porto confronta questo stato con l'occupazione attesa e ti
aiuta ad agire: rivendere i posti delle barche fuori, intercettare arrivi non registrati, verificare gli arrivi dei
transiti.

## Dispositivi

*Sensori* raccoglie lo stato dei posti, gli avvisi e le proposte, i **dispositivi**, i **gateway** e le **sorgenti**.
Un dispositivo si associa a un posto, a un tratto di area dinamica o a una colonnina; alla creazione ricevi la sua
**chiave**, mostrata una sola volta, da caricare sul sensore. Porto non è legato a un produttore: va bene qualsiasi
sensore o gateway che segua il [protocollo aperto](/api/ingestione-sensori/).

## Stato fisico

Un posto diventa occupato o libero solo dopo letture concordi (predefinito: 2 letture o 5 minuti), per non scambiare il
moto ondoso per un arrivo. Un sensore che tace diventa "sconosciuto". Le soglie si cambiano nelle impostazioni dei
sensori. Il contesto *Sensori* della mappa mostra liberi, occupati, discordanze e sensori offline o con batteria bassa.

## Riconciliazione

| Situazione | Che cosa propone Porto |
|---|---|
| Posto occupato senza nessuna occupazione prevista | avviso di posto occupato non previsto |
| Transito atteso oggi e posto che si occupa | registrare l'arrivo |
| Posto di un contratto annuale libero a lungo | presenza "fuori" e, se il contratto lo consente, il rilascio delle notti |
| Posto rilasciato e rivenduto che si occupa prima dell'arrivo del transito | avviso di rientro anticipato |
| Sensore muto o con batteria scarica | un intervento di manutenzione |

**I sensori propongono, non decidono**: nessuna lettura cambia contratti o occupazioni senza la conferma di un
operatore. L'unico effetto automatico, se lo abiliti, è sulla presenza.

## Storico e report

Lo storico di ogni sensore mostra lo stato fisico notte per notte confrontato con quello contrattuale; il report
*Sensori* riassume le discordanze. Le letture grezze si conservano per 30 giorni (configurabile), poi restano solo i
cambi di stato.
