# Yard quote no. {{quote.number}}

{{logo}}

Dear **{{customer.display_name}}**, here is our quote for the yard work on **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}**, length {{vessel.loa | meters}}, beam {{vessel.beam | meters}}.

{{#if quote.work_order_number}}Variation of work order **{{quote.work_order_number}}**.{{/if}}

| # | Description | Qty | Price | Discount | Net | VAT |
|---:|---:|---:|---:|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} {{this.unit}} | {{this.unit_price | money}} | {{this.discount | money}} | {{this.net | money}} | {{this.vat_rate | bp}} |
{{/each}}

| Rate | Net | VAT | Total |
|---:|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}}{{#if this.vat_nature}} {{this.vat_nature}}{{/if}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}

Net {{net | money}}, VAT {{vat_total | money}}, **total {{total | money}}**{{#if deposit}}, deposit on acceptance {{deposit | money}}{{/if}}.

{{#if hours}}Estimated hours: {{#each hours}}{{this.qualification}} {{this.hours}} h; {{/each}}{{/if}}

{{#if quote.planned_start_on}}Planned start {{quote.planned_start_on | date "long"}}{{#if quote.planned_delivery_on}}, planned delivery {{quote.planned_delivery_on | date "long"}}{{/if}}.{{/if}}

This quote is valid until {{quote.valid_until | date "long"}}.

{{#if quote.conditions}}
### Conditions

{{quote.conditions}}
{{/if}}

{{#if link}}You can accept it online: {{link}}{{/if}}

{{#if quote.notes}}
{{quote.notes}}
{{/if}}
