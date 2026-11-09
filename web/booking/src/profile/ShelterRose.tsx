import { useT } from '@porto/shared/i18n';

import type { ProfileWind } from '@porto/shared/api';

// Wedges of a compass rose in a 100×100 view box: polygon and label position per wind.
const WEDGES: { wind: ProfileWind; points: string; x: number; y: number }[] = [
  { wind: 'N', points: '50,50 32,8 68,8', x: 50, y: 22 },
  { wind: 'NE', points: '50,50 68,8 92,32', x: 70, y: 30 },
  { wind: 'E', points: '50,50 92,32 92,68', x: 78, y: 50 },
  { wind: 'SE', points: '50,50 92,68 68,92', x: 70, y: 70 },
  { wind: 'S', points: '50,50 68,92 32,92', x: 50, y: 78 },
  { wind: 'SW', points: '50,50 32,92 8,68', x: 30, y: 70 },
  { wind: 'W', points: '50,50 8,68 8,32', x: 22, y: 50 },
  { wind: 'NW', points: '50,50 8,32 32,8', x: 30, y: 30 },
];

/** Compass rose of the winds the harbour is sheltered from (filled wedges). */
export function ShelterRose({ shelter }: { shelter: ProfileWind[] }) {
  const { t } = useT();
  const label = `${t('publicProfile:shelter')}: ${shelter.join(', ')}`;
  return (
    <svg
      viewBox="0 0 100 100"
      role="img"
      aria-label={label}
      className="size-40"
      data-testid="shelter-rose"
    >
      {WEDGES.map((w) => (
        <g key={w.wind} data-wind={w.wind} data-sheltered={shelter.includes(w.wind)}>
          <polygon
            points={w.points}
            className={
              shelter.includes(w.wind)
                ? 'fill-primary stroke-background'
                : 'fill-muted stroke-background'
            }
            strokeWidth={1}
          />
          <text
            x={w.x}
            y={w.y}
            textAnchor="middle"
            dominantBaseline="middle"
            fontSize={7}
            className={
              shelter.includes(w.wind) ? 'fill-primary-foreground' : 'fill-muted-foreground'
            }
          >
            {w.wind}
          </text>
        </g>
      ))}
    </svg>
  );
}
