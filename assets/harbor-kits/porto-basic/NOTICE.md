# porto-basic harbour illustration kit

Original line symbols drawn for Porto (spec 7.7, m1-plan §2.7, D24).

Copyright © Coste360 SRL. Released under the same licence as the Porto repository (see `LICENSE` at the
repository root). The symbols use only geometric SVG elements and `currentColor`; colours come from the theme
tokens of the panel. Coste360 may replace the drawings later without changing the asset ids.

`web/scripts/gen-harbor-kit.mjs` checks and sanitises these files and generates
`web/shared/src/map/kit/generated/`; `internal/domain/layout/internal/genkit` generates the list of valid asset
ids for the backend (`make gen`).
