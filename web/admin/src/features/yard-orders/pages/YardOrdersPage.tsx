import { Link } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useWorkOrders,
  useYardBoard,
  type WorkOrderStatus,
  type YardBoardCard,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatBp, formatCm, formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import { Progress } from '@porto/shared/ui/progress';
import { TableCell, TableRow } from '@porto/shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import { EnumSelect, IsoDate, PageFrame } from '../../contracts-m4/components/kit';
import { OrderActions, OrderStatusBadge } from '../components/kit';
import { hoursText } from '../lib/orders';

const STATUSES: readonly WorkOrderStatus[] = [
  'planned',
  'in_progress',
  'on_hold',
  'completed',
  'delivered',
  'invoiced',
  'cancelled',
];

/** "Barche in lavorazione" (m10-plan §14.2): the board by status with the action menu, and the list with filters. */
export function YardOrdersPage() {
  const { t } = useT();
  const { can } = useCurrentMarina();
  return (
    <RequirePermission allowed={can('yard.read') || can('yard.manage')}>
      <PageFrame>
        <div data-testid="yard-orders-page" className="flex flex-col gap-4">
          <PageHeader title={t('yardOrders:title')} description={t('yardOrders:description')} />
          <Tabs defaultValue="board">
            <TabsList>
              <TabsTrigger value="board">{t('yardOrders:views.board')}</TabsTrigger>
              <TabsTrigger value="list">{t('yardOrders:views.list')}</TabsTrigger>
            </TabsList>
            <TabsContent value="board">
              <BoardView />
            </TabsContent>
            <TabsContent value="list">
              <ListView />
            </TabsContent>
          </Tabs>
        </div>
      </PageFrame>
    </RequirePermission>
  );
}

function BoardView() {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const board = useYardBoard(marinaId);
  if (board.isPending) return <LoadingState rows={4} />;
  if (board.isError)
    return <QueryErrorState error={board.error} onRetry={() => void board.refetch()} />;
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" data-testid="yard-board">
      {board.data.columns.map((col) => (
        <section
          key={col.status}
          aria-label={t(`yardOrders:status.${col.status}`)}
          className="flex flex-col gap-2"
        >
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            {t(`yardOrders:status.${col.status}`)}
            <Badge variant="secondary">{col.cards.length}</Badge>
          </h2>
          {col.cards.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('yardOrders:empty.column')}</p>
          ) : (
            col.cards.map((c) => <BoardCard key={c.work_order_id} card={c} />)
          )}
        </section>
      ))}
    </div>
  );
}

function BoardCard({ card }: { card: YardBoardCard }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  return (
    <Card data-testid={`board-card-${card.number}`}>
      <CardHeader className="flex flex-row items-start justify-between gap-2">
        <CardTitle className="text-base">
          <Link
            to="/m/$marinaId/yard/orders/$orderId"
            params={{ marinaId, orderId: card.work_order_id }}
            className="underline-offset-4 hover:underline"
          >
            {card.number}
          </Link>
          <span className="text-muted-foreground block text-sm font-normal">
            {card.vessel_name ?? ''}
          </span>
        </CardTitle>
        <OrderActions orderId={card.work_order_id} status={card.status} />
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        <div className="flex items-center gap-2">
          <Progress value={card.completion_bp / 100} aria-label={t('yardOrders:card.completion')} />
          <span className="tabular-nums">{formatBp(card.completion_bp, i18n.language)}</span>
        </div>
        <div className="text-muted-foreground flex flex-wrap gap-x-3 gap-y-1">
          {card.loa_cm ? (
            <span>{t('yardOrders:card.loa', { value: formatCm(card.loa_cm, i18n.language) })}</span>
          ) : null}
          {card.planned_delivery_on ? (
            <span>
              {t('yardOrders:card.delivery', { date: formatIsoDate(card.planned_delivery_on) })}
            </span>
          ) : null}
          <span>
            {t('yardOrders:card.hours', {
              approved: hoursText(card.hours_approved_milli, i18n.language),
              estimated: hoursText(card.hours_estimated_milli, i18n.language),
            })}
          </span>
        </div>
        {card.delay_days ? (
          <Badge variant="destructive">
            {t('yardOrders:card.delay', { count: card.delay_days })}
          </Badge>
        ) : null}
      </CardContent>
    </Card>
  );
}

function ListView() {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const [status, setStatus] = useState<WorkOrderStatus | ''>('');
  const [q, setQ] = useState('');
  const list = useWorkOrders(marinaId, { status: status || undefined, q: q || undefined });
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <EnumSelect
          id="wo-status"
          label={t('yardOrders:filters.status')}
          values={STATUSES}
          value={status}
          onChange={setStatus}
          labelOf={(v) => t(`yardOrders:status.${v}`)}
          allLabel={t('yardOrders:filters.all')}
        />
        <Field className="w-auto min-w-56">
          <FieldLabel htmlFor="wo-q">{t('yardOrders:filters.search')}</FieldLabel>
          <Input id="wo-q" value={q} maxLength={64} onChange={(e) => setQ(e.target.value)} />
        </Field>
      </div>
      {list.isPending ? (
        <LoadingState rows={4} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <SimpleTable
          head={[
            t('yardOrders:fields.number'),
            t('yardOrders:fields.vessel'),
            t('yardOrders:fields.status'),
            t('yardOrders:fields.plannedDelivery'),
            t('yardOrders:fields.completion'),
            t('yardOrders:fields.hours'),
          ]}
          empty={t('yardOrders:empty.orders')}
          rows={list.data.items.length}
        >
          {list.data.items.map((o) => (
            <TableRow key={o.id}>
              <TableCell>
                <Link
                  to="/m/$marinaId/yard/orders/$orderId"
                  params={{ marinaId, orderId: o.id }}
                  className="underline-offset-4 hover:underline"
                >
                  {o.number}
                </Link>
              </TableCell>
              <TableCell>{o.vessel_name ?? ''}</TableCell>
              <TableCell>
                <OrderStatusBadge status={o.status} />
              </TableCell>
              <TableCell>
                <IsoDate date={o.planned_delivery_on} />
              </TableCell>
              <TableCell className="tabular-nums">
                {formatBp(o.completion_bp, i18n.language)}
              </TableCell>
              <TableCell className="tabular-nums">
                {hoursText(o.hours_approved_milli, i18n.language)} /{' '}
                {hoursText(o.hours_estimated_milli, i18n.language)}
              </TableCell>
            </TableRow>
          ))}
        </SimpleTable>
      )}
    </div>
  );
}
