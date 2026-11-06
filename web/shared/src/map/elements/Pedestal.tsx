import { memo } from 'react';

import type { Pedestal as PedestalEl } from '../../api/schema';
import { PEDESTAL_SIZE_CM } from '../scene';
import { MAP_TOKENS, elementClass, type ElementState } from '../tokens';
import { ElementShell } from './common';

export type PedestalProps = {
  refId: string;
  el: PedestalEl;
  state: ElementState;
  interactive: boolean;
  label: string;
};

export const PedestalElement = memo(function PedestalElement({
  refId,
  el,
  state,
  interactive,
  label,
}: PedestalProps) {
  const s = PEDESTAL_SIZE_CM;
  return (
    <ElementShell
      refId={refId}
      kind="pedestal"
      state={state}
      interactive={interactive}
      label={label}
    >
      <rect
        x={el.position.x - s / 2}
        y={el.position.y - s / 2}
        width={s}
        height={s}
        rx={s / 5}
        className={elementClass(MAP_TOKENS.pedestal, state)}
        strokeWidth={1}
        vectorEffect="non-scaling-stroke"
      />
    </ElementShell>
  );
});
