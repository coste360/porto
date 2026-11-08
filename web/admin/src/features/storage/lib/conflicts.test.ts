import { describe, expect, it } from 'vitest';

import { ProblemError } from '@porto/shared/api';

import { conflictsOf } from './conflicts';

describe('scenario conflicts', () => {
  it('reads the full conflict list of a 409 scenario_conflicts only', () => {
    const conflicts = [
      { allocation_id: 'a', reason: 'stale' },
      { allocation_id: 'b', reason: 'resource_taken', cells: [1, 2] },
      { allocation_id: 'c', reason: 'overlap', with_allocation_ids: ['b'] },
    ];
    expect(
      conflictsOf(
        new ProblemError({ type: 'scenario_conflicts', status: 409, details: { conflicts } }),
      ),
    ).toEqual(conflicts);
    expect(conflictsOf(new ProblemError({ type: 'scenario_not_draft', status: 409 }))).toEqual([]);
    expect(conflictsOf(null)).toEqual([]);
  });
});
