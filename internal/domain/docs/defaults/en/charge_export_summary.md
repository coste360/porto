# Charge export summary {{number}}

{{#if kind == "reexport"}}Repeated export{{else}}Export to invoice{{/if}} of the period {{period_from | date}} – {{period_to | date}}{{#if filters}} ({{filters}}){{/if}}.

Charges: **{{charge_count}}** · lines: {{line_count}} · net {{net | money}} · VAT {{vat_total | money}} · total **{{total | money}}**

Charges are Porto's internal references: invoices are issued by the invoicing software.

## VAT summary

| Rate | Nature | Net | VAT | Total |
|---:|---|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}} | {{this.vat_nature}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}

## By origin

| Origin | Charges | Total |
|---|---:|---:|
{{#each by_origin}}| {{this.key}} | {{this.count}} | {{this.gross | money}} |
{{/each}}

## By revenue class

| Class | Lines | Total |
|---|---:|---:|
{{#each by_revenue_class}}| {{this.key}} | {{this.count}} | {{this.gross | money}} |
{{/each}}

## Charges

| Number | Date | Customer | Origin | Net | VAT | Total | Corrects |
|---|---|---|---|---:|---:|---:|---|
{{#each charges}}| {{this.number}} | {{this.issued_on | date}} | {{this.customer}} | {{this.origin_type}} | {{this.net | money}} | {{this.vat | money}} | {{this.total | money}} | {{this.corrects}} |
{{/each}}
