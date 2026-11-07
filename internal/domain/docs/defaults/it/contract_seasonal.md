# Contratto di ormeggio stagionale n. {{contract.code}}

{{logo}}

Tra **{{#if marina.legal_name}}{{marina.legal_name}}{{else}}{{marina.name}}{{/if}}**{{#if marina.vat_number}}, P. IVA {{marina.vat_number}}{{/if}}, di seguito "il Porto", e **{{customer.display_name}}{{#if customer.fiscal_code}}, C.F. {{customer.fiscal_code}}{{/if}}{{#if customer.vat_number}}, P. IVA {{customer.vat_number}}{{/if}}{{#if customer.address}}, {{customer.address}}{{/if}}**, di seguito "il Cliente", si conviene quanto segue.

## Oggetto

Il Porto concede al Cliente l'ormeggio dell'unità **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, lunghezza {{vessel.loa | meters}}, larghezza {{vessel.beam | meters}}** dal {{contract.starts_on | date "long"}} al {{contract.ends_on | date "long"}} (data di fine esclusa) nei seguenti posti:

| Posto | Struttura | Lunghezza | Dal | Al |
|---|---|---:|---|---|
{{#each berths}}| {{this.code}} | {{this.structure}} | {{this.length | meters}} | {{this.first_night | date}} | {{this.last_night | date}} |
{{/each}}

{{#if fleet}}
## Flotta

Flotta **{{fleet.name}}**: {{fleet.vessels | count}} unità, cambio equipaggi il giorno {{fleet.changeover_weekday}} della settimana.
{{/if}}

## Corrispettivo

| Voce | Quantità | Importo |
|---|---:|---:|
{{#each price.lines}}| {{this.description}} | {{this.qty}} {{this.unit}} | {{this.amount | money}} |
{{/each}}

Totale: **{{price.total | money}}** (imponibile {{price.net | money}}, IVA {{price.vat | money}}).

## Rate

{{#if installments}}
| Rata | Emissione | Scadenza | Importo |
|---:|---|---|---:|
{{#each installments}}| {{this.seq}} | {{this.issue_on | date}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
{{else}}
Pagamento in un'unica soluzione.
{{/if}}

I pagamenti si effettuano con bonifico{{#if marina.bank.iban}} sul conto IBAN {{marina.bank.iban}} intestato a {{marina.bank.holder}}{{/if}}, indicando come causale il numero del contratto.

## Condizioni

- Preavviso di disdetta: {{policy.cancellation_notice_days}} giorni.
{{#if policy.tacit_renewal}}- Il contratto si rinnova tacitamente salvo disdetta con {{policy.renewal_notice_days}} giorni di preavviso.
{{/if}}- Il Cliente dichiara di aver ricevuto il regolamento del porto e le condizioni generali.

{{#if contract.notes}}
## Note

{{contract.notes}}
{{/if}}

Luogo e data: ______________________, {{today | date "long"}}

{{signature "customer"}}

{{signature "marina"}}
