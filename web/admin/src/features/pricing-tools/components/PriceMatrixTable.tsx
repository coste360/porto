import type { components } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatCm, formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { cellErrorReason } from '../lib/enums';
import { money, numberLocale } from '../lib/format';

// Shared matrix table (m2-plan §7.2): the matrix page renders it with values, the renewal preview
// (WEB-PRICELISTS) in diff mode. It only shows what the server computed: every value is a cell total.

type S = components['schemas'];
type Matrix = S['PriceMatrix'];
type Cell = S['PriceMatrixCell'];
type DiffCell = S['RenewalPreview']['diffs'][number]['cells'][number];

export type PriceMatrixTableProps =
  | {
      mode: 'values';
      matrix: S['PriceMatrix'];
      onCellClick?: (probe: S['PricingSimulateRequest']) => void;
    }
  | {
      mode: 'diff';
      before: S['PriceMatrix'];
      after: S['PriceMatrix'];
      cells: S['RenewalPreview']['diffs'][number]['cells'];
    };

const cellKey = (row: number, col: number) => `${row}:${col}`;

function indexCells<T extends { row: number; col: number }>(cells: readonly T[]): Map<string, T> {
  return new Map(cells.map((c) => [cellKey(c.row, c.col), c]));
}

/** Column header: rate period code, the year / season column of annual and seasonal lists, or the single
 * column of a list without periods. */
function useColumnLabel() {
  const { t } = useT();
  return (key: string) =>
    key === 'year' || key === 'season' || key === 'all'
      ? t(`pricingTools:matrix.columns.${key}`)
      : key;
}

export function PriceMatrixTable(props: PriceMatrixTableProps) {
  const { t, i18n } = useT();
  const lang = i18n.language;
  const columnLabel = useColumnLabel();
  const matrix: Matrix = props.mode === 'values' ? props.matrix : props.after;
  const values = props.mode === 'values' ? indexCells(props.matrix.cells) : null;
  const diffs = props.mode === 'diff' ? indexCells(props.cells) : null;
  const beforeCells = props.mode === 'diff' ? indexCells(props.before.cells) : null;
  const afterCells = indexCells(matrix.cells);

  if (matrix.rows.length === 0 || matrix.columns.length === 0) {
    return (
      <p className="text-sm text-muted-foreground" data-testid="matrix-empty">
        {t('pricingTools:matrix.empty')}
      </p>
    );
  }

  return (
    <div data-mode={props.mode}>
      <Table data-testid="price-matrix">
        <TableHeader>
          <TableRow>
            <TableHead scope="col">{t('pricingTools:matrix.rowHeader')}</TableHead>
            {matrix.columns.map((c, ci) => (
              <TableHead key={ci} scope="col" className="text-right">
                <span className="block">{columnLabel(c.key)}</span>
                <span className="block text-xs font-normal text-muted-foreground">
                  {formatIsoDate(c.arrival)}
                </span>
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {matrix.rows.map((r, ri) => (
            <TableRow key={ri}>
              <TableHead scope="row">
                <span className="block">{r.key}</span>
                <span className="block text-xs font-normal text-muted-foreground">
                  {t('pricingTools:matrix.upTo', {
                    length: formatCm(r.probe_loa_cm, numberLocale(lang)),
                  })}
                </span>
              </TableHead>
              {matrix.columns.map((c, ci) => {
                const label = `${r.key} × ${columnLabel(c.key)}`;
                if (values) {
                  return (
                    <ValueCell
                      key={ci}
                      cell={values.get(cellKey(ri, ci))}
                      label={label}
                      onCellClick={props.mode === 'values' ? props.onCellClick : undefined}
                    />
                  );
                }
                return (
                  <DiffValueCell
                    key={ci}
                    diff={diffs?.get(cellKey(ri, ci))}
                    before={beforeCells?.get(cellKey(ri, ci))}
                    label={label}
                    afterCell={afterCells.get(cellKey(ri, ci))}
                  />
                );
              })}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function ValueCell({
  cell,
  label,
  onCellClick,
}: {
  cell: Cell | undefined;
  label: string;
  onCellClick: ((probe: S['PricingSimulateRequest']) => void) | undefined;
}) {
  const { t, i18n } = useT();
  if (!cell) return <TableCell className="text-right" />;
  const reason = cellErrorReason(cell.error);
  const text =
    cell.total_cents !== undefined
      ? money(cell.total_cents, i18n.language)
      : t(reason ? `pricingEnums:noPriceReasons.${reason}` : 'pricingTools:matrix.noPrice');
  const isError = cell.total_cents === undefined;
  return (
    <TableCell
      className="text-right tabular-nums"
      data-testid={`matrix-cell-${cell.row}-${cell.col}`}
      data-error={cell.error}
    >
      {onCellClick ? (
        <Button
          variant="ghost"
          size="sm"
          className={isError ? 'text-destructive' : undefined}
          aria-label={t('pricingTools:matrix.openInSimulator', { cell: label, value: text })}
          onClick={() => onCellClick(cell.probe)}
        >
          {text}
        </Button>
      ) : (
        <span className={isError ? 'text-destructive' : undefined}>{text}</span>
      )}
      {cell.unit && !isError ? (
        <span className="block text-xs text-muted-foreground">
          {t(`pricingEnums:timeUnits.${cell.unit}`)}
        </span>
      ) : null}
    </TableCell>
  );
}

function DiffValueCell({
  diff,
  before,
  afterCell,
  label,
}: {
  diff: DiffCell | undefined;
  before: Cell | undefined;
  afterCell: Cell | undefined;
  label: string;
}) {
  const { t, i18n } = useT();
  const lang = i18n.language;
  const oldCents = diff?.old_cents ?? before?.total_cents;
  const newCents = diff?.new_cents ?? afterCell?.total_cents;
  const delta = diff?.delta_cents;
  const deltaText = delta === undefined ? '' : `${delta > 0 ? '+' : ''}${money(delta, lang)}`;
  const bpText =
    diff?.delta_bp === undefined
      ? ''
      : `${diff.delta_bp > 0 ? '+' : ''}${formatBp(diff.delta_bp, numberLocale(lang))}`;
  return (
    <TableCell
      className="text-right tabular-nums"
      data-testid={`matrix-diff-${diff?.row ?? afterCell?.row}-${diff?.col ?? afterCell?.col}`}
      aria-label={label}
    >
      <span className="block font-medium">
        {newCents !== undefined ? money(newCents, lang) : t('pricingTools:matrix.noPrice')}
      </span>
      <span className="block text-xs text-muted-foreground line-through">
        {oldCents !== undefined ? money(oldCents, lang) : t('pricingTools:matrix.noPrice')}
      </span>
      {deltaText ? (
        <span className="block text-xs">
          {deltaText}
          {bpText ? ` (${bpText})` : ''}
        </span>
      ) : null}
    </TableCell>
  );
}
