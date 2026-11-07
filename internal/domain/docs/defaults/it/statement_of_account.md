# Estratto conto

Spett.le **{{customer.display_name}}**{{#if customer.fiscal_code}} · C.F. {{customer.fiscal_code}}{{/if}}{{#if customer.vat_number}} · P. IVA {{customer.vat_number}}{{/if}}

Periodo dal {{from | date}} al {{to | date}}.

Saldo iniziale: **{{opening | money}}**

| Data | Tipo | Numero | Descrizione | Scadenza | Dare | Avere | Saldo |
|---|---|---|---|---|---:|---:|---:|
{{#each rows}}| {{this.date | date}} | {{this.kind}} | {{this.number}} | {{this.description}} | {{this.due_on | date}} | {{this.debit | money}} | {{this.credit | money}} | {{this.balance | money}} |
{{/each}}

Saldo finale: **{{closing | money}}**{{#if credit}} · credito disponibile {{credit | money}}{{/if}}

Gli addebiti sono riferimenti interni di Porto: le fatture sono emesse dal software di fatturazione della marina.
