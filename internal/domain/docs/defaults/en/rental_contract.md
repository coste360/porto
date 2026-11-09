# Rental contract {{rental.number}}

{{logo}}

Between **{{marina.name}}** (the marina) and **{{renter.display_name}}**{{#if renter.fiscal_code}}, tax code {{renter.fiscal_code}}{{/if}} (the renter).

### Rented unit

Unit **{{rental.unit_code}}**, {{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, length {{vessel.loa | meters}}, seats {{rental.seats}}.

Formula **{{rental.formula}}**: from {{rental.start}} to {{rental.end}}.
{{#if rental.skipper}}With skipper: {{rental.skipper}}.{{/if}}
{{#if rental.licence_required}}The renter declares to hold the boating licence the unit requires.{{/if}}

### Price

| # | Description | Qty | Amount |
|---:|---|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} | {{this.amount | money}} |
{{/each}}

Total (VAT included): **{{total | money}}**.

{{#if deposit}}Deposit: **{{deposit | money}}**, returned at the return unless damages are found.{{/if}}

### Conditions

{{#if rental.fuel_metered}}The fuel used is charged at the return from the tank level.{{/if}}
A return after the planned time is charged after {{rental.grace_minutes}} minutes of grace, in blocks of {{rental.late_step_minutes}} minutes.
Damages found at the return are charged on the repair estimate, also by retaining the deposit.
The renter undertakes to respect the harbour rules and the navigation regulations.

{{#if rental.notes}}
{{rental.notes}}
{{/if}}

Return code: {{rental.qr}}

{{signature "customer"}}
