import { useState } from 'react';

import {
  useRentalQuote,
  useRentalSettings,
  useRentalUnits,
  type RentalFormula,
  type RentalUnit,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { Money } from '../../contracts-m4/components/kit';
import { probeRange } from '../lib';

/**
 * Price coverage (m11-plan §14.1): for a sample date, each formula × unit is priced through quoteRental with the
 * range its kind accepts; a missing price list rule shows as "no price".
 */
export function CoverageDialog({
  marinaId,
  formulas,
  onClose,
}: {
  marinaId: string;
  formulas: readonly RentalFormula[];
  onClose: () => void;
}) {
  const { t } = useT();
  const units = useRentalUnits(marinaId);
  const [date, setDate] = useState<string | null>(new Date().toISOString().slice(0, 10));
  const list = (units.data?.items ?? []).filter((u) => u.status !== 'retired');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t('rentalFleet:coverage.title')}</DialogTitle>
          <DialogDescription>{t('rentalFleet:coverage.description')}</DialogDescription>
        </DialogHeader>
        <Field className="w-auto max-w-56">
          <FieldLabel htmlFor="cov-date">{t('rentalFleet:coverage.date')}</FieldLabel>
          <DatePicker id="cov-date" value={date} onChange={setDate} />
        </Field>
        <div className="max-h-96 overflow-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('rentalFleet:fields.formula')}</TableHead>
                {list.map((u) => (
                  <TableHead key={u.id}>{u.code}</TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {formulas
                .filter((f) => f.active)
                .map((f) => (
                  <TableRow key={f.id}>
                    <TableCell className="font-medium">{f.code}</TableCell>
                    {list.map((u) => (
                      <TableCell key={u.id}>
                        {date ? (
                          <CoverageCell marinaId={marinaId} formula={f} unit={u} date={date} />
                        ) : null}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CoverageCell({
  marinaId,
  formula,
  unit,
  date,
}: {
  marinaId: string;
  formula: RentalFormula;
  unit: RentalUnit;
  date: string;
}) {
  const { t } = useT();
  const settings = useRentalSettings(marinaId);
  const s = settings.data;
  const fits = formula.categories.length === 0 || formula.categories.includes(unit.category);
  const band = s?.bands.find((b) => b.code === formula.band_code);
  const range =
    s && fits ? probeRange(formula.kind, date, s.open_from_slot, s.open_to_slot, band) : null;
  const quote = useRentalQuote(
    marinaId,
    range
      ? {
          unit_id: unit.id,
          formula_id: formula.id,
          ...range,
          ...(formula.skipper === 'required' ? { with_skipper: true } : {}),
        }
      : null,
  );
  if (!fits) return <span className="text-muted-foreground">–</span>;
  const opt = quote.data?.items[0];
  if (!opt) return null;
  if (opt.ok)
    return (
      <span data-testid="coverage-ok">
        <Money cents={opt.total_cents} />
      </span>
    );
  return (
    <Badge variant="destructive" data-testid="coverage-missing">
      {t(`errors:${opt.problem ?? 'no_price'}`)}
    </Badge>
  );
}
