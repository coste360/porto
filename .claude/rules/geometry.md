---
paths:
  - "internal/geo/**"
  - "internal/domain/layout/**"
  - "internal/domain/fit/**"
  - "internal/domain/harbor/**"
  - "web/shared/src/map/**"
  - "testdata/footprint/**"
---
# Geometry (Porto)

Spec: 07 (geometric model), 08–09 (map editor and operational view); ADR 0003 (cells), ADR 0004 (SVG in scale);
`docs/m0-plan.md` §9.1; INV-02, INV-04.

- Coordinates and lengths are `geo.Cm` (`int64` centimetres), areas `geo.Cm2`. No floats in stored or compared
  geometry. y points down, origin top-left. Rotation is `rotation_dd` = `geo.Angle` (tenths of a degree, 0..3599);
  trigonometry uses the generated fixed-point table
  (`internal/geo/sintable_gen.go`, `go generate ./internal/geo/...`), never `math.Sin` at run time.
- Rounding of divisions is half-even, done in one helper; never ad hoc.
- `geo`, `layout` and `fit` are pure: no store, no clock, no randomness (depguard + forbidigo).
- Overlap and containment tests are exact on integers. Berths sharing a side do not overlap (`RectsOverlap` =
  interiors); `PolygonsIntersect` and `Contains` are inclusive. Pick the predicate deliberately.
- Property tests with `pgregory.net/rapid` for every geometric predicate (disjoint cells ⇒ disjoint footprints;
  every point of a footprint lies in an occupied cell; generators produce no overlaps and unique codes). Run
  with `-rapid.checks=2000` before declaring done.
- Go ↔ TypeScript parity: shared JSON cases in `testdata/footprint/` run by both implementations of
  `Footprint`, `fit` and silhouettes; a change on one side updates both and the cases.
- SVG renders in cm units with a viewBox; culling and levels of detail keep 3,000 elements fluid.
