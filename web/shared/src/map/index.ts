// Shared harbour map renderer (m1-plan §5.2, ADR 0004): used by the viewer, the editor, the print page and,
// from M3, the operational view.
export * from './geometry';
export * from './footprint';
export * from './layers';
export * from './scene';
export * from './spatialIndex';
export * from './tokens';
export * from './viewport';
export {
  HarborMap,
  LAYER_OF_KIND,
  indexScene,
  type HarborMapMeta,
  type HarborMapProps,
} from './HarborMap';
export { useMapViewport, type MapViewport } from './useMapViewport';
export { backgroundSizeCm } from './elements/Background';
export { niceScaleLength } from './elements/Overlays';
export { KIT_ASSETS, KitIcon, isKitAsset, type KitAssetId } from './kit/KitDefs';
// M3 operational view (m3-plan §8.2).
export * from './lenses';
export * from './fit';
export * from './vessel-shape';
export * from './dayView';
export { LensFill, lensLod, LENS_FILL_PX, LENS_FULL_PX, type LensLod } from './elements/LensFill';
export { Vessel } from './elements/Vessel';
export { DragGhost, type DragGhostProps, type GhostState } from './elements/DragGhost';
export { GlyphIcon, GLYPH_IDS } from './elements/Glyphs';
