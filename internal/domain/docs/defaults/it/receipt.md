# Ricevuta di cortesia

**{{marina.name}}**{{#if marina.legal_name}} · {{marina.legal_name}}{{/if}}{{#if marina.vat_number}} · P. IVA {{marina.vat_number}}{{/if}}

Pagamento **{{number}}** del {{date | date}}{{#if customer}} · {{customer.display_name}}{{/if}}

| Descrizione | Aliquota | Importo |
|---|---:|---:|
{{#each lines}}| {{this.description}} | {{this.vat_rate | bp}} | {{this.gross | money}} |
{{/each}}

**Totale {{amount | money}}** · metodo {{method}}

{{#if vat}}| Aliquota | Natura | Imponibile | IVA | Totale |
|---:|---|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}} | {{this.vat_nature}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}{{/if}}

{{#if fiscal_number}}Documento commerciale n. {{fiscal_number}}.{{/if}}

{{footer}}

**Non valido ai fini fiscali.**
