import { describe, expect, it } from 'vitest';

import {
  detectDelimiter,
  guessMapping,
  mappingFromNames,
  mappingToNames,
  parseCsv,
  piersInFile,
  reportToCsv,
  toContractMapping,
} from './csv';

describe('parseCsv', () => {
  it('handles quotes, doubled quotes, CRLF, a BOM and blank lines', () => {
    const text = '﻿codice;pontile;note\r\nA-1;A;"uno; due"\r\n\r\nA-2;A;"dice ""ciao"""\n';
    expect(parseCsv(text, 'semicolon')).toEqual([
      ['codice', 'pontile', 'note'],
      ['A-1', 'A', 'uno; due'],
      ['A-2', 'A', 'dice "ciao"'],
    ]);
  });

  it('keeps a last line without newline and empty fields', () => {
    expect(parseCsv('a,b\n1,', 'comma')).toEqual([
      ['a', 'b'],
      ['1', ''],
    ]);
    expect(parseCsv('x\ty', 'tab')).toEqual([['x', 'y']]);
  });
});

describe('detectDelimiter', () => {
  it.each([
    ['code,pier,length\nA1,A,12', 'comma'],
    ['codice;pontile;lunghezza\nA1;A;12,5', 'semicolon'],
    ['code\tpier\tlength\nA1\tA\t12', 'tab'],
  ] as const)('%j → %s', (text, expected) => {
    expect(detectDelimiter(text)).toBe(expected);
  });
});

describe('column mapping', () => {
  it('guesses Italian and English headers', () => {
    expect(
      guessMapping(['Codice', 'Pontile', 'Lunghezza (m)', 'Larghezza', 'Profondità', 'Classe']),
    ).toEqual({ code: 0, pier: 1, length: 2, width: 3, depth: 4, price_class: 5 });
    expect(guessMapping(['Pier', 'Berth', 'Width', 'Length', 'Side', 'Mooring type'])).toEqual({
      pier: 0,
      code: 1,
      width: 2,
      length: 3,
      side: 4,
      mooring_type: 5,
    });
  });

  it('remembers mappings by header name', () => {
    const header = ['Posto', 'Molo', 'L', 'W'];
    const names = mappingToNames(header, { code: 0, pier: 1, length: 2, width: 3 });
    expect(mappingFromNames(['W', 'L', 'Molo', 'Posto'], names)).toEqual({
      code: 3,
      pier: 2,
      length: 1,
      width: 0,
    });
  });

  it('needs code, pier, length and width', () => {
    expect(toContractMapping({ code: 0, pier: 1, length: 2 })).toBeNull();
    expect(toContractMapping({ code: 0, pier: 1, length: 2, width: 3, side: 4 })).toEqual({
      code: 0,
      pier: 1,
      length: 2,
      width: 3,
      side: 4,
    });
  });

  it('lists the piers of the file in order', () => {
    expect(
      piersInFile(
        [
          ['1', 'B'],
          ['2', 'A'],
          ['3', 'B'],
          ['4', ' '],
        ],
        { pier: 1 },
      ),
    ).toEqual(['B', 'A']);
  });
});

describe('reportToCsv', () => {
  it('writes one line per rejected field, duplicate and planned berth, quoting when needed', () => {
    const csv = reportToCsv({
      rows_total: 3,
      valid: 1,
      rejected: [{ row: 2, errors: [{ field: 'length', code: 'invalid' }] }],
      duplicates: [{ row: 3, code: 'A,1', reason: 'code_in_file' }],
      planned: [{ row: 1, code: 'A-1', pier: 'A', side: 'left' }],
    });
    expect(csv.split('\r\n')).toEqual([
      'result,row,code,pier,side,field,detail',
      'rejected,2,,,,length,invalid',
      'duplicate,3,"A,1",,,code,code_in_file',
      'planned,1,A-1,A,left,,',
      '',
    ]);
  });
});
