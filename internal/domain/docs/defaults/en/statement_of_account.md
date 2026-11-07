# Statement of account

To **{{customer.display_name}}**{{#if customer.fiscal_code}} · Tax code {{customer.fiscal_code}}{{/if}}{{#if customer.vat_number}} · VAT {{customer.vat_number}}{{/if}}

Period from {{from | date}} to {{to | date}}.

Opening balance: **{{opening | money}}**

| Date | Type | Number | Description | Due | Debit | Credit | Balance |
|---|---|---|---|---|---:|---:|---:|
{{#each rows}}| {{this.date | date}} | {{this.kind}} | {{this.number}} | {{this.description}} | {{this.due_on | date}} | {{this.debit | money}} | {{this.credit | money}} | {{this.balance | money}} |
{{/each}}

Closing balance: **{{closing | money}}**{{#if credit}} · available credit {{credit | money}}{{/if}}

Charges are Porto's internal references: invoices are issued by the marina's invoicing software.
