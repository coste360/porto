# Renewal proposal

{{logo}}

Dear **{{customer.display_name}}**, we offer to renew contract {{contract.code}} for the vessel **{{vessel.name}}**.

| | Current | New |
|---|---|---|
| Period | {{contract.starts_on | date}} – {{contract.ends_on | date}} | {{new_contract.starts_on | date}} – {{new_contract.ends_on | date}} |
| Amount | {{old_price.total | money}} | **{{new_price.total | money}}** |

{{#if early_payment_total}}Paying in a single instalment: {{early_payment_total | money}}.{{/if}}

{{#if message}}
{{message}}
{{/if}}

Please reply by **{{deadline | date "long"}}**{{#if link}}: {{link}}{{/if}}.
