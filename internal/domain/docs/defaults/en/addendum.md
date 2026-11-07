# Addendum no. {{addendum.seq}} to contract {{contract.code}}

Between **{{marina.name}}** and **{{customer.display_name}}**, effective from {{addendum.effective_on | date "long"}}, the contract is amended as follows.

{{addendum.description}}

{{#if addendum.new_vessel}}New vessel: **{{addendum.new_vessel.name}}** ({{addendum.new_vessel.loa | meters}}).{{/if}}

Previous amount {{addendum.old_total | money}}, new amount **{{addendum.new_total | money}}** (difference {{addendum.delta | money}}).

{{#if installments}}
| No. | Due | Amount |
|---:|---|---:|
{{#each installments}}| {{this.seq}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
{{/if}}

All other conditions of the contract are unchanged.

{{signature "customer"}}

{{signature "marina"}}
