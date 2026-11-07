import { useMemo, useState } from 'react';

import { useSwapAllocations, type MoveResultData } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Label } from '@porto/shared/ui/label';
import { ElementCombobox } from '@porto/shared/ui/patterns';

import { OpsProblem } from './OpsProblem';

export type SwapCandidate = { allocationId: string; version: number; label: string };

/** "Scambia con…" (m3-plan §8.5): swap two vessels from a night in one transaction. */
export function SwapDialog(p: {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  a: SwapCandidate;
  candidates: readonly SwapCandidate[];
  preset?: string;
  fromNight: string;
  onSwapped: (r: MoveResultData) => void;
}) {
  const { t } = useT();
  const swap = useSwapAllocations(p.marinaId);
  const [b, setB] = useState<string | null>(p.preset ?? null);
  const options = useMemo(
    () =>
      p.candidates
        .filter((c) => c.allocationId !== p.a.allocationId)
        .map((c) => ({ value: c.allocationId, label: c.label })),
    [p.candidates, p.a.allocationId],
  );
  const other = p.candidates.find((c) => c.allocationId === b);
  return (
    <Dialog open={p.open} onOpenChange={p.onOpenChange}>
      <DialogContent data-testid="ops-swap-dialog">
        <DialogHeader>
          <DialogTitle>{t('opsMap:swap.title', { vessel: p.a.label })}</DialogTitle>
          <DialogDescription>
            {t('opsMap:swap.description', { date: p.fromNight })}
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-2">
          <Label htmlFor="ops-swap-with">{t('opsMap:swap.with')}</Label>
          <ElementCombobox
            id="ops-swap-with"
            options={options}
            value={b}
            onChange={setB}
            placeholder={t('opsMap:move.targetPlaceholder')}
            searchPlaceholder={t('opsMap:move.targetSearch')}
            emptyText={t('opsMap:move.targetEmpty')}
          />
          <OpsProblem error={swap.error} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => p.onOpenChange(false)}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!other || swap.isPending}
            data-testid="ops-swap-confirm"
            onClick={() =>
              other &&
              swap.mutate(
                {
                  body: {
                    a: { allocation_id: p.a.allocationId, version: p.a.version },
                    b: { allocation_id: other.allocationId, version: other.version },
                    from_night: p.fromNight,
                  },
                },
                {
                  onSuccess: (r) => {
                    p.onOpenChange(false);
                    p.onSwapped(r);
                  },
                },
              )
            }
          >
            {t('opsMap:swap.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
