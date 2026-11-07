# Payment request

**{{customer.display_name}}**

Please pay the following instalment.

| No. | Due | Amount |
|---:|---|---:|
| {{installment.seq}} | {{installment.due_on | date}} | **{{installment.amount | money}}** |

{{#if contract}}Contract {{contract.code}}.{{/if}}{{#if right}}Berth right on berth {{right.berth.code}}.{{/if}}

Bank transfer{{#if marina.bank.iban}} to IBAN **{{marina.bank.iban}}** held by {{marina.bank.holder}}{{/if}}, reference **{{payment_reference}}**.

{{#if overdue}}The instalment is overdue.{{/if}}
