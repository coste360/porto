# Preventivo n. {{quote.code}}

{{logo}}

Gentile **{{customer.display_name}}**, ti proponiamo l'ormeggio dell'unità **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, lunghezza {{vessel.loa | meters}}, larghezza {{vessel.beam | meters}}** dal {{quote.starts_on | date "long"}} al {{quote.ends_on | date "long"}}.

{{#each options}}
### Opzione {{this.seq}}{{#if this.label}}: {{this.label}}{{/if}}

| Posto | Struttura | Dal | Al |
|---|---|---|---|
{{#each this.placements}}| {{this.code}} | {{this.structure}} | {{this.first_night | date}} | {{this.last_night | date}} |
{{/each}}

| Voce | Importo |
|---|---:|
{{#each this.price.lines}}| {{this.description}} | {{this.amount | money}} |
{{/each}}

Totale **{{this.price.total | money}}**{{#if this.price.deposit}}, acconto alla firma {{this.price.deposit | money}}{{/if}}.
{{/each}}

Il preventivo è valido fino al {{valid_until | date "long"}}.

{{#if link}}Puoi accettarlo online: {{link}}{{/if}}

{{#if quote.notes}}
{{quote.notes}}
{{/if}}
