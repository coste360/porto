import { useEffect, useMemo, useState } from 'react';

import {
  checkPlacement,
  useAllocation,
  useApi,
  useMoveAllocation,
  type MoveResultData,
  type PlacementCheckData,
  type PlacementInputData,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import {
  DatePicker,
  ElementCombobox,
  LengthInput,
  type ComboboxOption,
} from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';

import { maxDate } from '../lib/dates';
import { OpsProblem } from './OpsProblem';

export type MoveTarget = {
  resourceType: PlacementInputData['resource_type'];
  resourceId: string;
  label: string;
  area?: boolean;
};

export type MoveDialogProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  allocationId: string;
  version: number;
  vesselName: string;
  /** Night shown on the map (default "from"). */
  date: string;
  today: string;
  /** Target dropped on the map; absent = keyboard alternative with a combobox. */
  target?: PlacementInputData;
  targets: readonly MoveTarget[];
  canOverride: boolean;
  onMoved: (r: MoveResultData) => void;
};

type When = 'from_today' | 'from_date' | 'interval';

/**
 * "Da quando" dialog of a move (spec 9.4): from today, from a date or only for an interval, with the advisory
 * check (fit, conflicts, price effect) before moveAllocation. The server rechecks everything.
 */
export function MoveDialog(p: MoveDialogProps) {
  const { t, i18n } = useT();
  const api = useApi();
  const alloc = useAllocation(p.marinaId, p.open ? p.allocationId : undefined);
  const move = useMoveAllocation(p.marinaId);
  const [when, setWhen] = useState<When>('from_today');
  const start = maxDate(p.date, p.today);
  const [from, setFrom] = useState(start);
  const [to, setTo] = useState<string | null>(null);
  const [targetId, setTargetId] = useState<string | null>(p.target?.resource_id ?? null);
  const [xCm, setXCm] = useState<number | null>(p.target?.x_cm ?? null);
  const [reason, setReason] = useState('');
  const [check, setCheck] = useState<PlacementCheckData | null>(null);

  const picked = p.targets.find((x) => x.resourceId === targetId);
  const target: PlacementInputData | null = p.target
    ? p.target
    : picked
      ? {
          resource_type: picked.resourceType,
          resource_id: picked.resourceId,
          ...(picked.area && xCm !== null ? { x_cm: Math.round(xCm / 10) * 10 } : {}),
        }
      : null;
  const fromNight = when === 'from_today' ? p.today : from;
  const lastNight = alloc.data?.last_night;
  const toNight = when === 'interval' ? (to ?? undefined) : undefined;
  const checkKey = target ? JSON.stringify([target, fromNight, toNight ?? lastNight]) : '';

  useEffect(() => {
    if (!p.open || !target || !(toNight ?? lastNight)) return;
    let live = true;
    checkPlacement(api, p.marinaId, {
      allocation_id: p.allocationId,
      target,
      from_night: fromNight,
      to_night: (toNight ?? lastNight)!,
    })
      .then((r) => live && setCheck(r))
      .catch(() => live && setCheck(null));
    return () => {
      live = false;
    };
    // checkKey covers target, from and to.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [checkKey, p.open, api, p.marinaId, p.allocationId]);

  const blockedFit = check && !check.fit.ok;
  const needsOverride = !!blockedFit && check!.fit.overridable && p.canOverride;
  const lang = i18n.language;
  const options: ComboboxOption[] = useMemo(
    () => p.targets.map((x) => ({ value: x.resourceId, label: x.label })),
    [p.targets],
  );

  const submit = () => {
    if (!target) return;
    move.mutate(
      {
        body: {
          allocation_id: p.allocationId,
          version: p.version,
          target,
          from_night: fromNight,
          ...(toNight ? { to_night: toNight } : {}),
          ...(needsOverride && reason.trim() ? { fit_override: { reason: reason.trim() } } : {}),
        },
      },
      {
        onSuccess: (r) => {
          p.onOpenChange(false);
          p.onMoved(r);
        },
      },
    );
  };

  return (
    <Dialog open={p.open} onOpenChange={p.onOpenChange}>
      <DialogContent data-testid="ops-move-dialog">
        <DialogHeader>
          <DialogTitle>{t('opsMap:move.title', { vessel: p.vesselName })}</DialogTitle>
          <DialogDescription>{t('opsMap:move.description')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          {!p.target ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="ops-move-target">{t('opsMap:move.target')}</Label>
              <ElementCombobox
                id="ops-move-target"
                options={options}
                value={targetId}
                onChange={setTargetId}
                placeholder={t('opsMap:move.targetPlaceholder')}
                searchPlaceholder={t('opsMap:move.targetSearch')}
                emptyText={t('opsMap:move.targetEmpty')}
              />
              {picked?.area ? (
                <>
                  <Label htmlFor="ops-move-x">{t('opsMap:move.x')}</Label>
                  <LengthInput id="ops-move-x" locale={lang} value={xCm} onChange={setXCm} />
                </>
              ) : null}
            </div>
          ) : (
            <p className="text-sm">
              {t('opsMap:move.to', {
                target: p.targets.find((x) => x.resourceId === p.target!.resource_id)?.label ?? '',
              })}
            </p>
          )}
          <RadioGroup
            value={when}
            onValueChange={(v) => setWhen(v as When)}
            aria-label={t('opsMap:move.when')}
          >
            {(['from_today', 'from_date', 'interval'] as const).map((w) => (
              <div key={w} className="flex items-center gap-2">
                <RadioGroupItem id={`ops-move-${w}`} value={w} />
                <Label htmlFor={`ops-move-${w}`}>{t(`opsMap:move.${w}`)}</Label>
              </div>
            ))}
          </RadioGroup>
          {when !== 'from_today' ? (
            <div className="grid grid-cols-2 gap-2">
              <div className="flex flex-col gap-1">
                <Label>{t('opsMap:move.fromNight')}</Label>
                <DatePicker
                  value={from}
                  onChange={(d) => d && setFrom(maxDate(d, p.today))}
                  aria-label={t('opsMap:move.fromNight')}
                />
              </div>
              {when === 'interval' ? (
                <div className="flex flex-col gap-1">
                  <Label>{t('opsMap:move.toNight')}</Label>
                  <DatePicker value={to} onChange={setTo} aria-label={t('opsMap:move.toNight')} />
                </div>
              ) : null}
            </div>
          ) : null}
          {check ? (
            <div className="flex flex-col gap-2" data-testid="ops-move-check" data-ok={check.ok}>
              {check.conflicts.map((c) => (
                <Alert key={`${c.resource_id}-${c.from}`} variant="destructive">
                  <AlertTitle>{t('occupancyEnums:conflict.title')}</AlertTitle>
                  <AlertDescription>
                    {t('occupancyEnums:conflict.nights', { from: c.from, to: c.to })}
                  </AlertDescription>
                </Alert>
              ))}
              {check.placement_error ? (
                <Alert variant="destructive">
                  <AlertDescription>
                    {t(`occupancyEnums:placementError.${check.placement_error}`)}
                  </AlertDescription>
                </Alert>
              ) : null}
              {[...check.fit.blocking, ...check.fit.warnings].map((i) => (
                <p key={i.code} className="text-sm">
                  {t(`occupancyEnums:fit.${i.code}`)}
                </p>
              ))}
              {check.price_effect ? (
                <p className="text-sm" data-testid="ops-move-price">
                  {t('opsMap:move.priceEffect', {
                    old: formatCents(check.price_effect.old_total_cents, 'EUR', lang),
                    next: formatCents(check.price_effect.new_total_cents, 'EUR', lang),
                    delta: formatCents(check.price_effect.delta_cents, 'EUR', lang),
                  })}
                </p>
              ) : null}
            </div>
          ) : null}
          {needsOverride ? (
            <div className="flex flex-col gap-1">
              <Label htmlFor="ops-move-reason">{t('opsMap:move.overrideReason')}</Label>
              <Input
                id="ops-move-reason"
                value={reason}
                maxLength={200}
                onChange={(e) => setReason(e.target.value)}
              />
            </div>
          ) : null}
          <OpsProblem error={move.error} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => p.onOpenChange(false)}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            onClick={submit}
            disabled={
              !target ||
              move.isPending ||
              (when === 'interval' && !to) ||
              (!!blockedFit && !(needsOverride && reason.trim()))
            }
            data-testid="ops-move-confirm"
          >
            {t('opsMap:move.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
