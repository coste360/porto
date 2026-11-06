import { memo } from 'react';

import type { Polygon } from '../../api/schema';
import { svgPoints } from '../geometry';
import { MAP_TOKENS } from '../tokens';

/** Water and land polygons of the map. */
export const WaterLand = memo(function WaterLand({
  water,
  land,
}: {
  water: Polygon[];
  land: Polygon[];
}) {
  return (
    <g aria-hidden="true">
      {water.map((p, i) => (
        <polygon
          key={`w${i}`}
          points={svgPoints(p)}
          className={MAP_TOKENS.water}
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
        />
      ))}
      {land.map((p, i) => (
        <polygon
          key={`l${i}`}
          points={svgPoints(p)}
          className={MAP_TOKENS.land}
          strokeWidth={1}
          vectorEffect="non-scaling-stroke"
        />
      ))}
    </g>
  );
});

/** Concession perimeter. */
export const ConcessionOutline = memo(function ConcessionOutline({
  outline,
}: {
  outline: Polygon;
}) {
  return (
    <polygon
      points={svgPoints(outline)}
      className={MAP_TOKENS.concession}
      strokeWidth={2}
      strokeDasharray="10 5"
      vectorEffect="non-scaling-stroke"
      aria-hidden="true"
    />
  );
});
