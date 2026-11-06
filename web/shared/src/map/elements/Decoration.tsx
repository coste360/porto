import { memo } from 'react';

import type { Decoration as DecorationEl } from '../../api/schema';
import { formatCm } from '../geometry';
import { isKitAsset, kitSymbolId } from '../kit/KitDefs';
import { DEFAULT_DECORATION_SIZE_CM } from '../scene';
import { MAP_TOKENS, elementClass, type ElementState } from '../tokens';
import { ElementShell, type Lod } from './common';

export type DecorationProps = {
  refId: string;
  el: DecorationEl;
  state: ElementState;
  lod: Lod;
  interactive: boolean;
  label: string;
  /** Prefix of the kit symbol ids of this map. */
  kitPrefix: string;
  locale: string;
};

/** Kit symbol (`<use>`), free text, depth label, north arrow or scale bar placed on the map. */
export const DecorationElement = memo(function DecorationElement({
  refId,
  el,
  state,
  lod,
  interactive,
  label,
  kitPrefix,
  locale,
}: DecorationProps) {
  const at = el.position ?? el.points?.[0] ?? { x: 0, y: 0 };
  const size = el.size_cm ?? DEFAULT_DECORATION_SIZE_CM;
  const deg = (el.rotation_dd ?? 0) / 10;
  const cls = elementClass(MAP_TOKENS.decoration, state);
  const transform = `translate(${at.x} ${at.y}) rotate(${deg})`;
  let body = null;
  switch (el.kind) {
    case 'symbol':
      body = isKitAsset(el.asset) ? (
        <use
          href={`#${kitSymbolId(kitPrefix, el.asset)}`}
          x={-size / 2}
          y={-size / 2}
          width={size}
          height={size}
          className={state === 'normal' ? 'text-foreground' : 'text-ring'}
        />
      ) : (
        <rect
          x={-size / 2}
          y={-size / 2}
          width={size}
          height={size}
          className={MAP_TOKENS.state.issue}
        />
      );
      break;
    case 'text': {
      const text = (locale.startsWith('en') ? el.label?.en : undefined) || el.label?.it || '';
      body =
        lod === 'labelled' ? (
          <text fontSize={size / 2} textAnchor="middle" dominantBaseline="central" className={cls}>
            {text}
          </text>
        ) : null;
      break;
    }
    case 'depth_label':
      body =
        lod !== 'labelled' ? null : (
          <text
            fontSize={size / 3}
            textAnchor="middle"
            dominantBaseline="central"
            className={MAP_TOKENS.labelMuted}
          >
            {formatCm(el.depth_cm ?? 0, locale)}
          </text>
        );
      break;
    case 'north_arrow':
      body = (
        <g className={cls}>
          <polygon
            points={`0,${-size / 2} ${size / 5},${size / 3} 0,${size / 6} ${-size / 5},${size / 3}`}
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
          {lod === 'labelled' ? (
            <text y={-size / 2 - size / 6} fontSize={size / 4} textAnchor="middle">
              N
            </text>
          ) : null}
        </g>
      );
      break;
    case 'scale_bar': {
      const len = size;
      body = (
        <g className={MAP_TOKENS.scaleBar}>
          <rect x={0} y={-len / 40} width={len / 2} height={len / 20} />
          <rect
            x={len / 2}
            y={-len / 40}
            width={len / 2}
            height={len / 20}
            className="fill-background"
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
          {lod === 'labelled' ? (
            <text x={len} y={-len / 15} fontSize={len / 10} textAnchor="end">
              {formatCm(len, locale)}
            </text>
          ) : null}
        </g>
      );
      break;
    }
  }
  return (
    <ElementShell
      refId={refId}
      kind="decoration"
      state={state}
      interactive={interactive}
      label={label}
      transform={transform}
    >
      {body}
    </ElementShell>
  );
});
