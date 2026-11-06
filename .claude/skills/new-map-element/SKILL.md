---
name: new-map-element
description: Add a new kind of harbour map element (structure, berth type, land resource, decoration, pedestal) from the geometric model to the editor and the operational view. Use from M1 when the map needs a new element type.
argument-hint: "[element kind]"
---
# New map element: $ARGUMENTS

Procedure for M1 and later; the packages named here do not exist in M0.

Read first: `docs/spec/07-mappa-modello-geometrico.md` (7.1 coordinates, 7.2 structures, 7.3 berths, 7.6 land
resources, 7.7 decorations and pedestals), `docs/spec/08-mappa-editor.md` (8.4 validations, 8.5 versions),
`docs/spec/09-mappa-vista-operativa.md`, ADR 0003 and 0004, `.claude/rules/geometry.md`.

1. **Model.** Decide the geometry with `internal/geo` types only: point, polyline, `geo.Rect` (anchor, width,
   length, rotation), polygon or circle, all in cm. Occupiable elements also define their cells/footprint
   (`internal/domain/layout`).
2. **Schema.** Table or `kind` value in the harbour tables via `/new-migration` (both dialects); geometry stored
   as integers or JSON of integers, always with `marina_id` and `map_id`.
3. **Validation.** Extend `ValidateMap` (8.4): no overlaps between occupiable elements, containment rules,
   codes unique per map. Property test (rapid) that generated maps containing the new element never overlap
   (INV-04).
4. **API.** Contract first (`/new-endpoint`), `ETag`/`If-Match` on the map version; changes with active
   occupancies follow 8.5.
5. **Event.** `config.map_changed` is emitted by the map service; do not add a per-element event without a
   spec change.
6. **Web.** SVG component in `web/shared/src/map` in cm units, culling and level of detail, token colours only;
   decorations come from `assets/harbor-kits` through the generated manifest. Editor handles (move, rotate,
   resize) snap in integer cm. i18n for every label.
7. **Parity.** If the element has a footprint or silhouette, add shared cases in `testdata/footprint/` run by Go
   and TypeScript.
8. **Performance.** Keep the Playwright map test at 3,000 elements within the frame budget.
9. `make check-full`; run the `invariants-guardian` subagent.
