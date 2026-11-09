import { useT } from '@porto/shared/i18n';

import type { PublicProfileMap } from '@porto/shared/api';

type Shape = PublicProfileMap['berths'][number];

function pointsOf(s: Shape): string {
  return s.points.map((p) => `${p[0]},${p[1]}`).join(' ');
}

const POI_RADIUS_CM = 250;

/**
 * The simplified public map (m12-plan §12.4) in map centimetres: structures, berth footprints, dynamic-area and
 * land outlines, points of interest. It shows no codes, occupancy, vessels or sensors: the data has none.
 */
export function SimpleMap({ map }: { map: PublicProfileMap }) {
  const { t } = useT();
  const w = Math.max(1, map.max_x_cm - map.min_x_cm);
  const h = Math.max(1, map.max_y_cm - map.min_y_cm);
  return (
    <svg
      viewBox={`${map.min_x_cm} ${map.min_y_cm} ${w} ${h}`}
      role="img"
      aria-label={t('publicProfile:mapAlt')}
      className="bg-muted/40 h-auto w-full rounded-md border"
      data-testid="simple-map"
    >
      <g data-layer="land">
        {map.land.map((s, i) => (
          <polygon
            key={`l${i}`}
            points={pointsOf(s)}
            className="fill-muted stroke-border"
            strokeWidth={10}
          />
        ))}
      </g>
      <g data-layer="areas">
        {map.areas.map((s, i) => (
          <polygon
            key={`a${i}`}
            points={pointsOf(s)}
            className="fill-accent stroke-border"
            strokeWidth={10}
          />
        ))}
      </g>
      <g data-layer="structures">
        {map.structures.map((s, i) =>
          s.closed ? (
            <polygon
              key={`s${i}`}
              points={pointsOf(s)}
              className="fill-secondary stroke-foreground"
              strokeWidth={10}
            />
          ) : (
            <polyline
              key={`s${i}`}
              points={pointsOf(s)}
              className="fill-none stroke-foreground"
              strokeWidth={s.width_cm ?? 100}
              strokeLinecap="square"
            />
          ),
        )}
      </g>
      <g data-layer="berths">
        {map.berths.map((s, i) => (
          <polygon
            key={`b${i}`}
            points={pointsOf(s)}
            className="fill-background stroke-primary"
            strokeWidth={10}
          >
            {s.category ? <title>{s.category}</title> : null}
          </polygon>
        ))}
      </g>
      <g data-layer="poi">
        {map.poi.map((p, i) => (
          <circle
            key={`p${i}`}
            cx={p.x_cm}
            cy={p.y_cm}
            r={POI_RADIUS_CM}
            className="fill-primary stroke-background"
            strokeWidth={40}
            data-kind={p.kind}
          >
            <title>{t(`publicProfile:poi.${p.kind}`)}</title>
          </circle>
        ))}
      </g>
    </svg>
  );
}
