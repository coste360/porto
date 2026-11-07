import { describe, expect, it } from 'vitest';

import { ProblemError, type Schemas } from '@porto/shared/api';

import { buildAudience, deliverable, draftFromAudience, EMPTY_AUDIENCE, toggle } from './audience';
import {
  checkCsvText,
  guessImportMapping,
  missingRequired,
  reportSummary,
  toApiMapping,
} from './imports';
import {
  PreviewUrl,
  enclosingEach,
  fieldTree,
  insertAt,
  markedLines,
  offsetOf,
  templateIssues,
  tokenFor,
} from './templateEditor';

const F = (path: string, type: Schemas['FieldType'], list = false): Schemas['FieldDef'] => ({
  path,
  type,
  list,
  description_key: `docsEnums:fields.${path}`,
});

const FIELDS = [
  F('customer', 'object'),
  F('customer.name', 'text'),
  F('customer.fiscal_code', 'text'),
  F('contract.total', 'cents'),
  F('installments', 'list', true),
  F('installments.amount', 'cents'),
  F('installments.due_on', 'date'),
  F('vessel.loa', 'cm'),
];

describe('templates editor: field tree insertion', () => {
  it('builds the catalogue tree from dotted paths', () => {
    const tree = fieldTree(FIELDS);
    expect(tree.map((n) => n.path)).toEqual(['customer', 'contract', 'installments', 'vessel']);
    expect(tree[0]!.children.map((n) => n.name)).toEqual(['fiscal_code', 'name']);
    expect(tree[1]!.field).toBeUndefined();
    expect(tree[2]!.field?.list).toBe(true);
  });

  it('inserts tokens with the default filter, each blocks for lists, this.x inside a list', () => {
    expect(tokenFor(F('customer.name', 'text'))).toBe('{{customer.name}}');
    expect(tokenFor(F('contract.total', 'cents'))).toBe('{{contract.total | money}}');
    expect(tokenFor(F('vessel.loa', 'cm'))).toBe('{{vessel.loa | meters}}');
    expect(tokenFor(F('installments', 'list', true))).toBe('{{#each installments}}\n- \n{{/each}}');
    expect(tokenFor(F('installments.amount', 'cents'), 'installments')).toBe(
      '{{this.amount | money}}',
    );
  });

  it('inserts at the selection and finds the enclosing each at the caret', () => {
    expect(insertAt('Gentile ,', 8, 8, '{{customer.name}}')).toEqual({
      value: 'Gentile {{customer.name}},',
      caret: 25,
    });
    expect(insertAt('abcXYZ', 3, 6, '!')).toEqual({ value: 'abc!', caret: 4 });
    const body = '{{#each installments}}\n- \n{{/each}}\nafter';
    expect(enclosingEach(body, body.indexOf('- ') + 2)).toBe('installments');
    expect(enclosingEach(body, body.length)).toBeUndefined();
  });
});

describe('templates editor: error markers by line', () => {
  it('reads template_invalid issues sorted by position and marks their lines', () => {
    const err = new ProblemError({
      type: 'template_invalid',
      status: 422,
      details: {
        issues: [
          { line: 3, col: 5, code: 'unknown_path', path: 'customer.nam' },
          { line: 1, col: 1, code: 'unclosed_block' },
        ],
      },
    });
    const issues = templateIssues(err);
    expect(issues.map((i) => [i.line, i.code])).toEqual([
      [1, 'unclosed_block'],
      [3, 'unknown_path'],
    ]);
    const lines = markedLines('a\nb\nc', issues);
    expect(lines.map((l) => l.issues.length)).toEqual([1, 0, 1]);
    expect(offsetOf('ab\ncd\nef', 3, 2)).toBe(7);
  });

  it('gives one generic issue when details are missing and none for other problems', () => {
    expect(templateIssues(new ProblemError({ type: 'template_limits', status: 422 }))).toEqual([
      { line: 0, col: 0, code: 'template_limits', part: 'body' },
    ]);
    expect(templateIssues(new ProblemError({ type: 'not_found', status: 404 }))).toEqual([]);
  });
});

describe('templates editor: preview blob handling', () => {
  it('keeps one object URL and revokes the previous one', () => {
    const revoked: string[] = [];
    let n = 0;
    const urls = new PreviewUrl(
      () => `blob:${++n}`,
      (u) => revoked.push(u),
    );
    expect(urls.set(new Blob(['%PDF'], { type: 'application/pdf' }))).toBe('blob:1');
    expect(urls.set(new Blob(['%PDF']))).toBe('blob:2');
    expect(revoked).toEqual(['blob:1']);
    urls.clear();
    expect(revoked).toEqual(['blob:1', 'blob:2']);
    expect(urls.url).toBeNull();
  });
});

describe('communications audience builder', () => {
  it('builds the audience of the selected mode only', () => {
    const d = { ...EMPTY_AUDIENCE, terms: ['annual' as const], customerIds: ['c1'] };
    expect(buildAudience(d)).toEqual({ ok: true, audience: { terms: ['annual'] } });
    expect(buildAudience({ ...d, mode: 'customers' })).toEqual({
      ok: true,
      audience: { customer_ids: ['c1'] },
    });
    expect(
      buildAudience({ ...d, mode: 'map', mapId: 'm1', resourceIds: ['b1', 'b1', 'b2'] }),
    ).toEqual({
      ok: true,
      audience: { map_id: 'm1', resource_ids: ['b1', 'b2'] },
    });
  });

  it('refuses empty filters, reversed periods and bad day counts', () => {
    expect(buildAudience(EMPTY_AUDIENCE)).toEqual({ ok: false, error: 'empty' });
    expect(
      buildAudience({ ...EMPTY_AUDIENCE, presentFrom: '2026-08-10', presentTo: '2026-08-01' }),
    ).toEqual({ ok: false, error: 'range' });
    expect(buildAudience({ ...EMPTY_AUDIENCE, deadlinesWithinDays: '0' })).toEqual({
      ok: false,
      error: 'days',
    });
    expect(buildAudience({ ...EMPTY_AUDIENCE, mode: 'map', mapId: 'm1' })).toEqual({
      ok: false,
      error: 'selection',
    });
    expect(buildAudience({ ...EMPTY_AUDIENCE, holders: true, deadlinesWithinDays: '30' })).toEqual({
      ok: true,
      audience: { holders: true, with_deadlines_within_days: 30 },
    });
  });

  it('restores the mode from a saved audience and counts deliverable recipients', () => {
    expect(draftFromAudience({ map_id: 'm1', resource_ids: ['b1'] }).mode).toBe('map');
    expect(draftFromAudience({ customer_ids: ['c'] }).mode).toBe('customers');
    expect(draftFromAudience({ groups: ['g'] })).toMatchObject({ mode: 'filter', groups: ['g'] });
    expect(toggle(['a', 'b'], 'a')).toEqual(['b']);
    expect(toggle(['a'], 'b')).toEqual(['a', 'b']);
    expect(deliverable({ count: 10, skipped_no_consent: 3, skipped_no_email: 2 })).toBe(5);
  });
});

describe('imports helpers', () => {
  it('guesses the mapping from the header and lists the missing required fields', () => {
    const m = guessImportMapping('rights', [
      'Berth code',
      'Kind',
      'From on',
      'Holder tax id',
      'Altro',
    ]);
    expect(m).toEqual({ berth_code: 0, kind: 1, from_on: 2, holder_tax_id: 3 });
    expect(missingRequired('rights', m)).toEqual(['share_millis']);
    expect(toApiMapping({ ...m, share_millis: undefined })).toEqual({
      berth_code: 0,
      kind: 1,
      from_on: 2,
      holder_tax_id: 3,
    });
  });

  it('checks the file and summarises a report', () => {
    expect(checkCsvText('', 0)).toEqual({ ok: false, error: 'empty' });
    expect(checkCsvText('a', 5 * 1024 * 1024)).toEqual({ ok: false, error: 'tooLarge' });
    const ok = checkCsvText('a;b\n1;2\n', 8);
    expect(ok.ok && ok.delimiter).toBe('semicolon');
    expect(
      reportSummary({
        entity: 'deadlines',
        dry_run: true,
        rows: 3,
        valid: 2,
        rejected: [{ row: 3, code: 'invalid_date' }],
        duplicates: [],
      }),
    ).toEqual({ rows: 3, valid: 2, rejected: 1, duplicates: 0, applicable: false });
  });
});
