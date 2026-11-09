# Contratto di noleggio {{rental.number}}

{{logo}}

Tra **{{marina.name}}** (il porto) e **{{renter.display_name}}**{{#if renter.fiscal_code}}, codice fiscale {{renter.fiscal_code}}{{/if}} (il noleggiatore).

### Unità noleggiata

Unità **{{rental.unit_code}}**, {{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, lunghezza {{vessel.loa | meters}}, posti {{rental.seats}}.

Formula **{{rental.formula}}**: dal {{rental.start}} al {{rental.end}}.
{{#if rental.skipper}}Con skipper: {{rental.skipper}}.{{/if}}
{{#if rental.licence_required}}Il noleggiatore dichiara di essere in possesso della patente nautica richiesta per l'unità.{{/if}}

### Prezzo

| # | Descrizione | Q.tà | Importo |
|---:|---|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} | {{this.amount | money}} |
{{/each}}

Totale (IVA inclusa): **{{total | money}}**.

{{#if deposit}}Cauzione: **{{deposit | money}}**, restituita al rientro salvo danni accertati.{{/if}}

### Condizioni

{{#if rental.fuel_metered}}Il carburante consumato è addebitato al rientro in base al livello del serbatoio.{{/if}}
La riconsegna oltre l'orario previsto comporta un supplemento dopo {{rental.grace_minutes}} minuti di tolleranza, a blocchi di {{rental.late_step_minutes}} minuti.
I danni accertati al rientro sono addebitati sulla base del preventivo di riparazione, anche trattenendo la cauzione.
Il noleggiatore si impegna a rispettare il regolamento del porto e le norme di navigazione.

{{#if rental.notes}}
{{rental.notes}}
{{/if}}

Codice di riconsegna: {{rental.qr}}

{{signature "customer"}}
