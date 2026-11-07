# Proposta di rinnovo

{{logo}}

Gentile **{{customer.display_name}}**, ti proponiamo il rinnovo del contratto {{contract.code}} per l'unità **{{vessel.name}}**.

| | Attuale | Nuovo |
|---|---|---|
| Periodo | {{contract.starts_on | date}} – {{contract.ends_on | date}} | {{new_contract.starts_on | date}} – {{new_contract.ends_on | date}} |
| Importo | {{old_price.total | money}} | **{{new_price.total | money}}** |

{{#if early_payment_total}}Pagando in un'unica soluzione: {{early_payment_total | money}}.{{/if}}

{{#if message}}
{{message}}
{{/if}}

Rispondi entro il **{{deadline | date "long"}}**{{#if link}}: {{link}}{{/if}}.
