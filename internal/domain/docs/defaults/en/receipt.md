# Courtesy receipt

**{{marina.name}}**{{#if marina.legal_name}} · {{marina.legal_name}}{{/if}}{{#if marina.vat_number}} · VAT {{marina.vat_number}}{{/if}}

Payment **{{number}}** of {{date | date}}{{#if customer}} · {{customer.display_name}}{{/if}}

| Description | Rate | Amount |
|---|---:|---:|
{{#each lines}}| {{this.description}} | {{this.vat_rate | bp}} | {{this.gross | money}} |
{{/each}}

**Total {{amount | money}}** · method {{method}}

{{#if vat}}| Rate | Nature | Net | VAT | Gross |
|---:|---|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}} | {{this.vat_nature}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}{{/if}}

{{#if fiscal_number}}Commercial document no. {{fiscal_number}}.{{/if}}

{{footer}}

**Non valido ai fini fiscali** (not valid for tax purposes).
