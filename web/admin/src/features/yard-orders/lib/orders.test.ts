import { describe, expect, it } from 'vitest';

import type { FinalAccount, Timesheet } from '@porto/shared/api';

import {
  actionsFor,
  approvable,
  groupAccount,
  needsReason,
  parseHours,
  parseQty,
  selectedApprovable,
} from './orders';

describe('work order helpers', () => {
  it('offers the actions of the state table', () => {
    expect(actionsFor('planned')).toEqual(['start', 'cancel']);
    expect(actionsFor('completed')).toEqual(['reopen', 'deliver']);
    expect(actionsFor('invoiced')).toEqual(['deliver']);
    expect(actionsFor('invoiced', '2027-06-02T10:00:00Z')).toEqual([]);
    expect(actionsFor('cancelled')).toEqual([]);
    expect(needsReason('cancel')).toBe('required');
    expect(needsReason('hold')).toBe('optional');
    expect(needsReason('start')).toBeNull();
  });

  it('selects the submitted entries only', () => {
    const e = (id: string, status: Timesheet['status']) =>
      ({ id, status, version: 2 }) as Timesheet;
    const list = [e('a', 'submitted'), e('b', 'approved'), e('c', 'submitted')];
    expect(approvable(list)).toEqual([
      { id: 'a', version: 2 },
      { id: 'c', version: 2 },
    ]);
    expect(selectedApprovable(list, new Set(['b', 'c']))).toEqual([{ id: 'c', version: 2 }]);
  });

  it('groups the final account by source', () => {
    const line = (source: string, revenue: number, cost: number) => ({
      source,
      revenue_cents: revenue,
      cost_cents: cost,
    });
    const acc = {
      lines: [
        line('service', 21600, 0),
        line('labour', 34200, 20600),
        line('labour', 24750, 14300),
      ],
    } as unknown as FinalAccount;
    const g = groupAccount(acc);
    expect(g.map((x) => x.source)).toEqual(['labour', 'service']);
    expect(g[0]).toMatchObject({ revenue: 58950, cost: 34900 });
  });

  it('parses hours in quarter steps and quantities', () => {
    expect(parseHours('4,25')).toBe(4250);
    expect(parseHours('5.5')).toBe(5500);
    expect(parseHours('4,1')).toBeNull();
    expect(parseHours('0')).toBeNull();
    expect(parseHours('x')).toBeNull();
    expect(parseQty('6')).toBe(6000);
    expect(parseQty('1,5')).toBe(1500);
    expect(parseQty('0')).toBeNull();
  });
});
