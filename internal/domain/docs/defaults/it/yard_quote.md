# Preventivo lavori n. {{quote.number}}

{{logo}}

Gentile **{{customer.display_name}}**, ti proponiamo i lavori di cantiere sull'unità **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}**, lunghezza {{vessel.loa | meters}}, larghezza {{vessel.beam | meters}}.

{{#if quote.work_order_number}}Variante della commessa **{{quote.work_order_number}}**.{{/if}}

| # | Descrizione | Q.tà | Prezzo | Sconto | Imponibile | IVA |
|---:|---:|---:|---:|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} {{this.unit}} | {{this.unit_price | money}} | {{this.discount | money}} | {{this.net | money}} | {{this.vat_rate | bp}} |
{{/each}}

| Aliquota | Imponibile | IVA | Totale |
|---:|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}}{{#if this.vat_nature}} {{this.vat_nature}}{{/if}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}

Imponibile {{net | money}}, IVA {{vat_total | money}}, **totale {{total | money}}**{{#if deposit}}, acconto all'accettazione {{deposit | money}}{{/if}}.

{{#if hours}}Ore stimate: {{#each hours}}{{this.qualification}} {{this.hours}} h; {{/each}}{{/if}}

{{#if quote.planned_start_on}}Inizio lavori previsto il {{quote.planned_start_on | date "long"}}{{#if quote.planned_delivery_on}}, consegna prevista il {{quote.planned_delivery_on | date "long"}}{{/if}}.{{/if}}

Il preventivo è valido fino al {{quote.valid_until | date "long"}}.

{{#if quote.conditions}}
### Condizioni

{{quote.conditions}}
{{/if}}

{{#if link}}Puoi accettarlo online: {{link}}{{/if}}

{{#if quote.notes}}
{{quote.notes}}
{{/if}}
