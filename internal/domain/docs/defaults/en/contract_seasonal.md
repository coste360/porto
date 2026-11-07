# Seasonal mooring contract no. {{contract.code}}

{{logo}}

Between **{{#if marina.legal_name}}{{marina.legal_name}}{{else}}{{marina.name}}{{/if}}**{{#if marina.vat_number}}, VAT {{marina.vat_number}}{{/if}}, hereinafter "the Marina", and **{{customer.display_name}}{{#if customer.fiscal_code}}, tax code {{customer.fiscal_code}}{{/if}}{{#if customer.vat_number}}, VAT {{customer.vat_number}}{{/if}}{{#if customer.address}}, {{customer.address}}{{/if}}**, hereinafter "the Customer", it is agreed as follows.

## Subject

The Marina grants the Customer the mooring of the vessel **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, length {{vessel.loa | meters}}, beam {{vessel.beam | meters}}** from {{contract.starts_on | date "long"}} to {{contract.ends_on | date "long"}} (end date excluded) on the following berths:

| Berth | Structure | Length | From | To |
|---|---|---:|---|---|
{{#each berths}}| {{this.code}} | {{this.structure}} | {{this.length | meters}} | {{this.first_night | date}} | {{this.last_night | date}} |
{{/each}}

{{#if fleet}}
## Fleet

Fleet **{{fleet.name}}**: {{fleet.vessels | count}} vessels, changeover on weekday {{fleet.changeover_weekday}}.
{{/if}}

## Price

| Item | Quantity | Amount |
|---|---:|---:|
{{#each price.lines}}| {{this.description}} | {{this.qty}} {{this.unit}} | {{this.amount | money}} |
{{/each}}

Total: **{{price.total | money}}** (net {{price.net | money}}, VAT {{price.vat | money}}).

## Instalments

{{#if installments}}
| No. | Issued | Due | Amount |
|---:|---|---|---:|
{{#each installments}}| {{this.seq}} | {{this.issue_on | date}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
{{else}}
Single payment.
{{/if}}

Payments are made by bank transfer{{#if marina.bank.iban}} to IBAN {{marina.bank.iban}} held by {{marina.bank.holder}}{{/if}}, quoting the contract number.

## Conditions

- Cancellation notice: {{policy.cancellation_notice_days}} days.
{{#if policy.tacit_renewal}}- The contract renews tacitly unless cancelled with {{policy.renewal_notice_days}} days' notice.
{{/if}}- The Customer declares to have received the harbour rules and the general conditions.

{{#if contract.notes}}
## Notes

{{contract.notes}}
{{/if}}

Place and date: ______________________, {{today | date "long"}}

{{signature "customer"}}

{{signature "marina"}}
