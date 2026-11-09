import { useNavigate } from '@tanstack/react-router';
import { ChevronLeftIcon, ChevronRightIcon, ListIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
  toProblem,
  useMoveRentalBooking,
  useRentalPlanner,
  type RentalPlannerView,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { addDays, buildAxis, PlannerGrid, slotLabel } from '@porto/shared/planner';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@porto/shared/ui/toggle-group';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { BookingDialog, type BookingDraft } from '../components/BookingDialog';
import { gridOf, viewDays, viewWindow } from '../lib/plan';

const VIEWS: readonly RentalPlannerView[] = ['hours', 'half_days', 'days'];
const CATEGORIES = ['rib', 'open', 'cabin', 'sail', 'pedalo', 'sup'] as const;
const ALL = '__all__';

/**
 * Rental planner (m11-plan §14.2): units grouped by category and the skippers by hours, half days or days; drag on
 * free cells to book (the dialog suggests the formula), drag a booking to move it (row = unit), click it to open
 * it. Live through SSE rental.changed (query invalidation). A 409 highlights the bookings in the way.
 */
export function RentalPlannerPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const navigate = useNavigate();
  const today = useMarinaToday();
  const [view, setView] = useState<RentalPlannerView>('hours');
  const [from, setFrom] = useState<string | null>(null);
  const [category, setCategory] = useState(ALL);
  const [draft, setDraft] = useState<BookingDraft | null>(null);
  const [highlight, setHighlight] = useState<string[]>([]);
  const start = from ?? today;
  const win = viewWindow(view, start);
  const planner = useRentalPlanner(
    marinaId,
    win.from,
    win.to,
    view,
    category === ALL ? undefined : (category as never),
  );
  const move = useMoveRentalBooking(marinaId);
  const p = planner.data;
  const axis = useMemo(
    () =>
      p
        ? buildAxis({
            view,
            from: win.from,
            days: viewDays(view),
            openFrom: p.open_from_slot,
            openTo: p.open_to_slot,
            bands: p.bands,
          })
        : null,
    [p, view, win.from],
  );
  const grid = useMemo(
    () =>
      p
        ? gridOf(p, {
            status: (s) => t(`rentalBookings:status.${s}`),
            kind: (k) => t(`rentalBookings:kind.${k}`),
            reason: (r) => t(`rentalBookings:reason.${r}`),
          })
        : null,
    [p, t],
  );
  const openBooking = (id: string) =>
    void navigate({
      to: '/m/$marinaId/rental/bookings/$bookingId',
      params: { marinaId, bookingId: id },
    });

  return (
    <RequirePermission allowed={can('rental.manage')}>
      <div className="flex w-full flex-col gap-4">
        <PageHeader
          title={t('rentalBookings:title')}
          description={t('rentalBookings:description')}
          actions={
            <Button
              variant="outline"
              onClick={() =>
                void navigate({ to: '/m/$marinaId/rental/bookings', params: { marinaId } })
              }
            >
              <ListIcon />
              {t('rentalBookings:nav.bookings')}
            </Button>
          }
        />
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-auto">
            <FieldLabel htmlFor="rp-from">{t('rentalBookings:planner.from')}</FieldLabel>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="icon"
                aria-label={t('rentalBookings:planner.previous')}
                onClick={() => setFrom(addDays(start, -viewDays(view)))}
              >
                <ChevronLeftIcon />
              </Button>
              <Input
                id="rp-from"
                type="date"
                className="w-40"
                value={start}
                onChange={(e) => setFrom(e.target.value || null)}
              />
              <Button
                variant="outline"
                size="icon"
                aria-label={t('rentalBookings:planner.next')}
                onClick={() => setFrom(addDays(start, viewDays(view)))}
              >
                <ChevronRightIcon />
              </Button>
              <Button variant="ghost" onClick={() => setFrom(null)}>
                {t('rentalBookings:planner.today')}
              </Button>
            </div>
          </Field>
          <ToggleGroup
            type="single"
            variant="outline"
            value={view}
            onValueChange={(v) => v && setView(v as RentalPlannerView)}
            aria-label={t('rentalBookings:planner.view')}
          >
            {VIEWS.map((v) => (
              <ToggleGroupItem key={v} value={v}>
                {t(`rentalBookings:planner.views.${v}`)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <Field className="w-48">
            <FieldLabel htmlFor="rp-category">{t('rentalBookings:planner.category')}</FieldLabel>
            <Select value={category} onValueChange={setCategory}>
              <SelectTrigger id="rp-category">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>{t('rentalBookings:planner.allCategories')}</SelectItem>
                {CATEGORIES.map((c) => (
                  <SelectItem key={c} value={c}>
                    {t(`rentalBookings:category.${c}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
        {planner.isLoading ? <LoadingState /> : null}
        {planner.isError ? (
          <QueryErrorState error={planner.error} onRetry={() => void planner.refetch()} />
        ) : null}
        {p && axis && grid ? (
          <>
            <PlannerGrid
              axis={axis}
              rows={grid.rows}
              segments={grid.segments}
              highlight={highlight}
              label={t('rentalBookings:planner.gridLabel')}
              dateLabel={(d) => formatIsoDate(d)}
              cellLabel={(r, i) => {
                const c = axis.cells[i]!;
                return t('rentalBookings:planner.cell', {
                  row: r.label,
                  date: formatIsoDate(c.date),
                  from: slotLabel(c.from),
                  to: slotLabel(c.to),
                });
              }}
              onCreate={(rowId, range) => {
                const row = p.rows.find((r) => r.id === rowId);
                setHighlight([]);
                setDraft({
                  unitId: rowId,
                  unitCode: row?.code ?? '',
                  category: row?.category,
                  range,
                });
              }}
              onMove={(bookingId, _from, rowId, range) => {
                setHighlight([]);
                move.mutate(
                  { id: bookingId, unit_id: rowId, start: range.start, end: range.end },
                  {
                    onSuccess: (b) =>
                      toast.success(t('rentalBookings:toast.moved', { number: b.number })),
                    onError: (err) => {
                      const pr = toProblem(err);
                      setHighlight((pr.details.booking_ids as string[] | undefined) ?? []);
                      toast.error(t(pr.i18nKey));
                    },
                  },
                );
              }}
              onSegment={(s) => s.bookingId && openBooking(s.bookingId)}
            />
            <Legend />
          </>
        ) : null}
        {draft && p ? (
          <BookingDialog
            marinaId={marinaId}
            draft={draft}
            openFrom={p.open_from_slot}
            openTo={p.open_to_slot}
            bands={p.bands}
            onConflict={setHighlight}
            onClose={() => setDraft(null)}
          />
        ) : null}
      </div>
    </RequirePermission>
  );
}

const LEGEND = [
  ['held', 'bg-chart-4/40 border-dashed border-chart-4'],
  ['confirmed', 'bg-chart-1'],
  ['checked_out', 'bg-chart-2'],
  ['returned', 'bg-chart-3'],
  ['closed', 'bg-muted'],
  ['no_show', 'bg-destructive/30'],
  ['buffer', 'bg-muted/60'],
  ['block', 'bg-secondary'],
] as const;

function Legend() {
  const { t } = useT();
  return (
    <ul
      className="text-muted-foreground flex flex-wrap gap-4 text-xs"
      aria-label={t('rentalBookings:planner.legend')}
    >
      {LEGEND.map(([k, cls]) => (
        <li key={k} className="flex items-center gap-1">
          <span className={`inline-block size-3 rounded-sm border ${cls}`} />
          {k === 'buffer' || k === 'block'
            ? t(`rentalBookings:kind.${k}`)
            : t(`rentalBookings:status.${k}`)}
        </li>
      ))}
    </ul>
  );
}
