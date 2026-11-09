import type { ReactNode } from 'react';
import {
  ArchiveIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  LockIcon,
  PinIcon,
  RotateCwIcon,
  UnlockIcon,
} from 'lucide-react';

import type { StorageAccessEntry, StorageItemIssue } from '@porto/shared/api';
import { cn } from '@porto/shared/lib/utils';
import { useT } from '@porto/shared/i18n';
import { formatCm, formatIsoDate } from '@porto/shared/lib/format';
import {
  ACCESSIBILITY_LEGEND,
  accessibilityStyles,
  blockedExitsByDate,
  TOKEN_CLASSES,
  type HarborToken,
} from '@porto/shared/map';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Label } from '@porto/shared/ui/label';
import { Slider } from '@porto/shared/ui/slider';
import { Switch } from '@porto/shared/ui/switch';

import { trayIds, type DraftAction, type DraftState } from '../lib/draft';
import { addDays, daysBetween, presentOn, timelineRows } from '../lib/plan';
import { PlanGrid, type PlanBoat } from './PlanGrid';
import { Timeline } from './Timeline';

// Split view of the planner and of the scenario editor (spec 17.2–17.4): plan at the selected date with the date
// slider and the day stepper, the timeline of the window, the Accessibilità lens, the "Da collocare" tray and the
// selected boat with the button alternatives of the keyboard (rotate, tray, pin).

export type WorkspaceMeta = {
  allocation_id: string;
  kind: string;
  vessel?: { name: string; loa_cm: number; beam_cm: number } | undefined;
  contract?: { code: string; term?: string } | undefined;
  first_night: string;
  last_night: string;
  arrival_on: string;
  exit_on: string;
  cells?: readonly number[];
};

export type PlannerWorkspaceProps = {
  idPrefix: string;
  from: string;
  to: string;
  date: string;
  onDate: (d: string) => void;
  grid: DraftState['grid'];
  draft: DraftState;
  dispatch: (a: DraftAction) => void;
  meta: readonly WorkspaceMeta[];
  exits: readonly number[];
  obstacles: { cells: readonly number[]; first_night: string; last_night: string }[];
  access: readonly StorageAccessEntry[];
  issues: readonly StorageItemIssue[];
  lens: boolean;
  onLens: (on: boolean) => void;
  readOnly?: boolean;
  /** Pin toggle (scenarios only). */
  pinnable?: boolean;
  toolbar?: ReactNode;
};

function tokenOf(m: WorkspaceMeta | undefined): HarborToken {
  if (!m) return 'muted';
  if (m.kind === 'transit') return 'transit';
  if (m.kind === 'owner') return 'owner';
  if (m.kind === 'hold') return 'hold';
  switch (m.contract?.term) {
    case 'seasonal':
      return 'seasonal';
    case 'monthly':
      return 'monthly';
    case 'period':
      return 'period';
  }
  return 'annual';
}

export function PlannerWorkspace(p: PlannerWorkspaceProps) {
  const { t, i18n } = useT();
  const metaById = new Map(p.meta.map((m) => [m.allocation_id, m]));
  const issueById = new Map(p.issues.map((i) => [i.allocation_id, i]));
  const days = Math.max(0, daysBetween(p.from, p.to));
  const styles = accessibilityStyles(p.access, p.date);
  const blockedByDate = blockedExitsByDate(p.access);
  const blockedExits = new Set(
    p.access.filter((a) => a.event === 'exit' && !a.accessible).map((a) => a.allocation_id),
  );
  const label = (id: string) =>
    metaById.get(id)?.vessel?.name ?? metaById.get(id)?.contract?.code ?? id.slice(0, 8);

  const boats: PlanBoat[] = p.draft.order
    .map((id) => p.draft.items[id]!)
    .filter((it) => {
      const m = metaById.get(it.allocationId);
      return it.at && (!m || presentOn(m, p.date));
    })
    .map((it) => {
      const m = metaById.get(it.allocationId);
      const orig = p.draft.original[it.allocationId];
      const unchanged =
        orig &&
        it.at &&
        orig.x === it.at.x &&
        orig.y === it.at.y &&
        orig.rotation === it.at.rotation;
      const issue = issueById.get(it.allocationId);
      return {
        item: it,
        label: label(it.allocationId),
        token: tokenOf(m),
        ...(unchanged && m?.cells ? { serverCells: m.cells } : {}),
        ...(issue ? { issue: issue.reason } : {}),
      };
    });
  const obstacles = p.obstacles.filter((o) => presentOn(o, p.date)).flatMap((o) => o.cells);
  const tray = trayIds(p.draft);
  const sel = p.draft.selected ? p.draft.items[p.draft.selected] : undefined;
  const selMeta = sel ? metaById.get(sel.allocationId) : undefined;
  const selIssue = sel ? issueById.get(sel.allocationId) : undefined;
  const selStyle = sel ? styles.get(sel.allocationId) : undefined;
  const dayWarnings = p.access.filter((a) => a.on === p.date && !a.accessible);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-4">
        <div className="flex min-w-64 flex-1 flex-col gap-2">
          <Label htmlFor={`${p.idPrefix}-date`}>
            {t('storage:planner.date', { date: formatIsoDate(p.date) })}
          </Label>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              aria-label={t('storage:planner.prevDay')}
              disabled={p.date <= p.from}
              onClick={() => p.onDate(addDays(p.date, -1))}
            >
              <ChevronLeftIcon />
            </Button>
            <Slider
              id={`${p.idPrefix}-date`}
              min={0}
              max={days}
              step={1}
              value={[Math.min(days, Math.max(0, daysBetween(p.from, p.date)))]}
              onValueChange={(v) => p.onDate(addDays(p.from, v[0] ?? 0))}
              aria-label={t('storage:planner.dateSlider')}
            />
            <Button
              variant="outline"
              size="sm"
              aria-label={t('storage:planner.nextDay')}
              disabled={p.date >= p.to}
              onClick={() => p.onDate(addDays(p.date, 1))}
            >
              <ChevronRightIcon />
            </Button>
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <Switch id={`${p.idPrefix}-lens`} checked={p.lens} onCheckedChange={p.onLens} />
            <Label htmlFor={`${p.idPrefix}-lens`}>{t('storage:planner.lens')}</Label>
          </div>
          {p.lens ? <AccessibilityLegend /> : null}
        </div>
        {p.toolbar}
      </div>
      <div className="grid gap-4 xl:grid-cols-3">
        <div className="flex flex-col gap-3 xl:col-span-2">
          <PlanGrid
            grid={p.grid}
            boats={boats}
            exits={p.exits}
            obstacles={obstacles}
            {...(p.draft.selected ? { selected: p.draft.selected } : {})}
            {...(p.lens ? { lens: styles } : {})}
            readOnly={!!p.readOnly}
            dispatch={p.dispatch}
            onSelect={(id) => p.dispatch({ type: 'select', ...(id ? { id } : {}) })}
            ariaLabel={t('storage:planner.planLabel', { date: formatIsoDate(p.date) })}
            boatLabel={(b) => {
              const s = styles.get(b.item.allocationId);
              return [
                b.label,
                b.issue ? t(`storage:issue.${b.issue}`) : '',
                s ? t(`storage:lens.${s.key}`) : '',
                p.readOnly ? '' : t('storage:planner.keyboardHint'),
              ]
                .filter(Boolean)
                .join('. ');
            }}
          />
          <p className="text-muted-foreground text-xs">{t('storage:planner.keyboardHelp')}</p>
          <Timeline
            rows={timelineRows(p.meta, blockedExits)}
            from={p.from}
            to={p.to}
            date={p.date}
            {...(p.draft.selected ? { selected: p.draft.selected } : {})}
            onSelect={(id) => p.dispatch({ type: 'select', id })}
            ariaLabel={t('storage:planner.timeline')}
            rowLabel={(r) =>
              t(r.exitBlocked ? 'storage:planner.rowBlocked' : 'storage:planner.row', {
                name: r.label,
                arrival: formatIsoDate(r.arrival),
                exit: formatIsoDate(r.exit),
              })
            }
          />
        </div>
        <div className="flex flex-col gap-4">
          <Card className="gap-2 py-4" data-testid="storage-selected">
            <CardHeader className="px-4">
              <CardTitle className="text-base">
                {sel ? label(sel.allocationId) : t('storage:planner.noSelection')}
              </CardTitle>
            </CardHeader>
            {sel ? (
              <CardContent className="flex flex-col gap-2 px-4 text-sm">
                {selMeta?.vessel ? (
                  <p>
                    {formatCm(selMeta.vessel.loa_cm, i18n.language)} ×{' '}
                    {formatCm(selMeta.vessel.beam_cm, i18n.language)}
                  </p>
                ) : null}
                {selMeta ? (
                  <p className="text-muted-foreground">
                    {t('storage:planner.stay', {
                      arrival: formatIsoDate(selMeta.arrival_on),
                      exit: formatIsoDate(selMeta.exit_on),
                    })}
                  </p>
                ) : null}
                {selIssue ? (
                  <Badge variant="destructive">
                    {t(`storage:issue.${selIssue.reason}`)}
                    {selIssue.fit_codes?.length
                      ? `: ${selIssue.fit_codes.map((c) => t(`occupancyEnums:fitCode.${c}`, { defaultValue: c })).join(', ')}`
                      : ''}
                  </Badge>
                ) : null}
                {selStyle && selStyle.key !== 'ok' ? (
                  <Badge variant={selStyle.key === 'blocked' ? 'destructive' : 'secondary'}>
                    {t(`storage:lens.${selStyle.key}`)}
                    {selStyle.blockedBy.length
                      ? ` (${selStyle.blockedBy.map(label).join(', ')})`
                      : ''}
                  </Badge>
                ) : null}
                {!p.readOnly ? (
                  <div className="flex flex-wrap gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!sel.at || sel.pinned}
                      onClick={() => p.dispatch({ type: 'rotate', id: sel.allocationId })}
                    >
                      <RotateCwIcon />
                      {t('storage:planner.rotate')}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!sel.at || sel.pinned}
                      onClick={() => p.dispatch({ type: 'unplace', id: sel.allocationId })}
                    >
                      <ArchiveIcon />
                      {t('storage:planner.toTray')}
                    </Button>
                    {p.pinnable ? (
                      <Button
                        variant="outline"
                        size="sm"
                        aria-pressed={sel.pinned}
                        onClick={() => p.dispatch({ type: 'togglePin', id: sel.allocationId })}
                      >
                        {sel.pinned ? <UnlockIcon /> : <PinIcon />}
                        {t(sel.pinned ? 'storage:planner.unpin' : 'storage:planner.pin')}
                      </Button>
                    ) : null}
                  </div>
                ) : null}
              </CardContent>
            ) : null}
          </Card>
          <Card className="gap-2 py-4" data-testid="storage-tray">
            <CardHeader className="px-4">
              <CardTitle className="text-base">
                {t('storage:planner.tray', { count: tray.length })}
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 px-4 text-sm">
              {tray.length === 0 ? (
                <p className="text-muted-foreground">{t('storage:planner.trayEmpty')}</p>
              ) : (
                tray.map((id) => (
                  <div key={id} className="flex items-center justify-between gap-2">
                    <span>{label(id)}</span>
                    {!p.readOnly ? (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          p.dispatch({ type: 'moveTo', id, x: 0, y: 0 });
                          p.dispatch({ type: 'select', id });
                        }}
                      >
                        {t('storage:planner.place')}
                      </Button>
                    ) : null}
                  </div>
                ))
              )}
            </CardContent>
          </Card>
          <Card className="gap-2 py-4" data-testid="storage-warnings">
            <CardHeader className="px-4">
              <CardTitle className="text-base">
                {t('storage:planner.warnings', { date: formatIsoDate(p.date) })}
              </CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-1 px-4 text-sm">
              {dayWarnings.length === 0 ? (
                <p className="text-muted-foreground">
                  {blockedByDate.size === 0
                    ? t('storage:planner.noWarnings')
                    : t('storage:planner.noWarningsToday', { count: blockedByDate.size })}
                </p>
              ) : (
                dayWarnings.map((w) => (
                  <p key={`${w.allocation_id}-${w.event}`} className="flex items-center gap-2">
                    <LockIcon className="text-destructive size-4" aria-hidden />
                    {t(
                      w.event === 'exit'
                        ? 'storage:warning.exit_blocked'
                        : 'storage:warning.arrival_blocked',
                      {
                        name: label(w.allocation_id),
                        blocking: w.blocking.map(label).join(', '),
                      },
                    )}
                  </p>
                ))
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}

/** Colour key of the Accessibilità lens (shown while the lens is on). */
const LEGEND_SWATCH: Record<(typeof ACCESSIBILITY_LEGEND)[number], string> = {
  blocked: TOKEN_CLASSES.bad.bg,
  blocking: cn(TOKEN_CLASSES.free.bg, 'ring-destructive opacity-40 ring-2'),
  leaving: TOKEN_CLASSES.departure.bg,
  arrival_blocked: TOKEN_CLASSES.warn.bg,
  ok: cn(TOKEN_CLASSES.free.bg, 'opacity-40'),
};

function AccessibilityLegend() {
  const { t } = useT();
  return (
    <ul
      className="text-muted-foreground flex flex-wrap gap-3 text-xs"
      aria-label={t('storage:planner.lens')}
      data-testid="storage-lens-legend"
    >
      {ACCESSIBILITY_LEGEND.map((k) => (
        <li key={k} className="flex items-center gap-1">
          <span className={cn('inline-block size-3 rounded-sm', LEGEND_SWATCH[k])} />
          {t(`storage:lens.${k}`)}
        </li>
      ))}
    </ul>
  );
}
