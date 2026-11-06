import { createElement, memo, type ReactElement } from 'react';

import { KIT_ASSETS, type KitAssetId } from './generated/manifest';
import { KIT_SYMBOLS, type KitNode } from './generated/symbols';

const KIT_IDS = new Set<string>(KIT_ASSETS.map((a) => a.id));

export function isKitAsset(id: string | undefined): id is KitAssetId {
  return !!id && KIT_IDS.has(id);
}

/** DOM id of a kit symbol inside one map (prefix from useId, so two maps on a page never clash). */
export function kitSymbolId(prefix: string, asset: string): string {
  return `${prefix}-kit-${asset}`;
}

/** kebab-case SVG attribute to the React prop name (stroke-width → strokeWidth). */
function propName(attr: string): string {
  return attr.replace(/-([a-z])/g, (_m, c: string) => c.toUpperCase());
}

/** Renders the sanitised JSON element tree of the generator: no innerHTML anywhere. */
export function renderKitNode(node: KitNode, key: number): ReactElement {
  const props: Record<string, string | number> = { key };
  for (const [name, value] of Object.entries(node.attrs)) props[propName(name)] = value;
  return createElement(
    node.tag,
    props,
    ...node.children.map((child, i) => renderKitNode(child, i)),
  );
}

export type KitDefsProps = {
  /** Prefix of the symbol ids (useId of the map). */
  prefix: string;
  /** Assets used on the map: only these symbols are emitted. */
  assets: ReadonlySet<string>;
};

/** `<defs>` with one `<symbol viewBox="0 0 100 100">` per used kit asset. */
export const KitDefs = memo(function KitDefs({ prefix, assets }: KitDefsProps) {
  const used = [...assets].filter(isKitAsset).sort();
  if (used.length === 0) return null;
  return (
    <defs>
      {used.map((id) => (
        <symbol key={id} id={kitSymbolId(prefix, id)} viewBox="0 0 100 100">
          {KIT_SYMBOLS[id].map((node, i) => renderKitNode(node, i))}
        </symbol>
      ))}
    </defs>
  );
});

/** Standalone icon of a kit asset (KitPicker, legend). */
export function KitIcon({ asset, className }: { asset: KitAssetId; className?: string }) {
  return (
    <svg viewBox="0 0 100 100" className={className} aria-hidden="true" focusable="false">
      {KIT_SYMBOLS[asset].map((node, i) => renderKitNode(node, i))}
    </svg>
  );
}

export { KIT_ASSETS, type KitAssetId };
