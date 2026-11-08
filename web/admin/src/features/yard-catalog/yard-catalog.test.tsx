import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, baseRoutes } from '../desk/test/fixtures';
import { column, i18nBody, issueKey, issuesFor, parseBandRows } from './lib';

describe('yard catalogue helpers', () => {
  it('maps issue codes and fields', () => {
    expect(issueKey('div_by_zero')).toBe('yardCatalog:issues.div_by_zero');
    expect(issueKey('whatever')).toBe('yardCatalog:issues.unknown');
    const issues = [
      { code: 'syntax', field: 'lines[0].qty_formula', pos: 3 },
      { code: 'unknown_variable', field: 'lines[1].price_formula', ref: 'nope' },
      { code: 'unknown_variable', field: 'job_vars', ref: 'x' },
    ];
    expect(issuesFor(issues, 'lines[0].qty_formula')).toHaveLength(1);
    expect(issuesFor(issues, 'lines[1]')).toHaveLength(1);
    expect(column(3)).toBe(4);
    expect(column(undefined)).toBeNull();
    expect(i18nBody(' Lavaggio ', '')).toEqual({ it: 'Lavaggio' });
  });

  it('parses band rows with the open last row', () => {
    expect(parseBandRows('10;100\n15,5;150\n;300')).toEqual([
      { up_to_milli: 10000, value_milli: 100000 },
      { up_to_milli: 15500, value_milli: 150000 },
      { up_to_milli: null, value_milli: 300000 },
    ]);
    expect(parseBandRows('10')).toBeNull();
    expect(parseBandRows('')).toBeNull();
  });
});

describe('yard catalogue page', () => {
  it('lists the packages and shows the formula issues of the catalogue', async () => {
    renderApp(`/m/${MARINA_ID}/yard/catalog`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'yard.read'])),
      {
        path: `${API}/yard/work-packages`,
        body: {
          items: [
            {
              id: 'p1',
              code: 'ANTIVEG',
              name: { it: 'Antivegetativa' },
              work_type_id: 'wt',
              active: true,
              current_version: 2,
              version: 3,
              created_at: '',
              updated_at: '',
            },
          ],
          next_cursor: null,
        },
      },
      { path: `${API}/yard/work-types`, body: { items: [] } },
    ]);
    const row = await screen.findByTestId('package-row');
    expect(row).toHaveTextContent('ANTIVEG');
    expect(row).toHaveTextContent('v2');
    expect(screen.queryByRole('button', { name: 'Nuovo gruppo di lavorazioni' })).toBeNull();
  });
});
