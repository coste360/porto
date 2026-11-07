# Richiesta di pagamento

Spett.le **{{customer.display_name}}**

Ti chiediamo il pagamento della rata seguente.

| Rata | Scadenza | Importo |
|---:|---|---:|
| {{installment.seq}} | {{installment.due_on | date}} | **{{installment.amount | money}}** |

{{#if contract}}Contratto {{contract.code}}.{{/if}}{{#if right}}Diritto d'uso sul posto {{right.berth.code}}.{{/if}}

Bonifico{{#if marina.bank.iban}} su IBAN **{{marina.bank.iban}}** intestato a {{marina.bank.holder}}{{/if}}, causale **{{payment_reference}}**.

{{#if overdue}}La rata risulta scaduta.{{/if}}
