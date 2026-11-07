import { createRoute } from '@tanstack/react-router';

import type { MarinaRoute } from '../../router';
import { AreaLayoutPage } from './AreaLayoutPage';
import { OpsPage } from './OpsPage';

// Operational map routes (m3-plan §8.1): the operational view and the layout session of a dynamic area.

export type OpsSearch = {
  map?: string;
  date?: string;
  lens?: string;
  focus?: string;
  mode?: 'scale' | 'schema';
};

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v.length > 0 && v.length <= 64 ? v : undefined;
}

/** Search params of /ops, validated loosely (unknown values are dropped). */
export function validateOpsSearch(search: Record<string, unknown>): OpsSearch {
  const out: OpsSearch = {};
  const map = str(search.map);
  const date = str(search.date);
  const lens = str(search.lens);
  const focus = str(search.focus);
  if (map) out.map = map;
  if (date) out.date = date;
  if (lens) out.lens = lens;
  if (focus) out.focus = focus;
  if (search.mode === 'scale' || search.mode === 'schema') out.mode = search.mode;
  return out;
}

/** Routes of the operational view, children of the marina route. */
export function operationsRoutes(parent: MarinaRoute) {
  return [
    createRoute({
      getParentRoute: () => parent,
      path: 'ops',
      validateSearch: validateOpsSearch,
      component: OpsPage,
    }),
    createRoute({
      getParentRoute: () => parent,
      path: 'ops/area/$areaId',
      validateSearch: (search: Record<string, unknown>): { date?: string } => {
        const date = str(search.date);
        return date ? { date } : {};
      },
      component: AreaLayoutPage,
    }),
  ] as const;
}
