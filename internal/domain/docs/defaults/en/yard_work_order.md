# Work order {{order.number}}

{{logo}}

Customer **{{customer.display_name}}**, vessel **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}**, length {{vessel.loa | meters}}.

{{#if order.quote_number}}From quote **{{order.quote_number}}**.{{/if}} Status: {{order.status}}, progress {{order.completion | bp}}.
{{#if order.position}}Position: {{order.position}}.{{/if}}

{{#if order.planned_start_on}}Planned start {{order.planned_start_on | date "long"}}{{#if order.planned_delivery_on}}, planned delivery {{order.planned_delivery_on | date "long"}}{{/if}}.{{/if}}

### Planned work

| # | Description | Qty | Price | Net |
|---:|---|---:|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} | {{this.unit_price | money}} | {{this.net | money}} |
{{/each}}

Planned net {{net | money}}.

### Tasks

| # | Task | Estimated hours | Progress | Status |
|---:|---|---:|---:|---|
{{#each tasks}}| {{this.seq}} | {{this.title}} | {{this.estimated_hours}} | {{this.progress | bp}} | {{this.status}} |
{{/each}}

{{#if charged}}Already charged: {{charged | money}}.{{/if}}

{{#if order.notes}}
{{order.notes}}
{{/if}}
