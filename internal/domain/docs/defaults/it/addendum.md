# Addendum n. {{addendum.seq}} al contratto {{contract.code}}

Tra **{{marina.name}}** e **{{customer.display_name}}**, con effetto dal {{addendum.effective_on | date "long"}}, il contratto è modificato come segue.

{{addendum.description}}

{{#if addendum.new_vessel}}Nuova unità: **{{addendum.new_vessel.name}}** ({{addendum.new_vessel.loa | meters}}).{{/if}}

Importo precedente {{addendum.old_total | money}}, nuovo importo **{{addendum.new_total | money}}** (differenza {{addendum.delta | money}}).

{{#if installments}}
| Rata | Scadenza | Importo |
|---:|---|---:|
{{#each installments}}| {{this.seq}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
{{/if}}

Restano ferme tutte le altre condizioni del contratto.

{{signature "customer"}}

{{signature "marina"}}
