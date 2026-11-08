import { useState } from 'react';

import {
  useTransitionWorkOrder,
  useWorkOrder,
  type WorkOrderAction,
  type WorkOrderStatus,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';

import { useCurrentMarina } from '../../../auth/hooks';
import { ReasonDialog } from '../../contracts-m4/components/kit';
import { actionsFor, needsReason } from '../lib/orders';

// Building blocks of the work order pages: the status badge and the action menu (state table transitions with a
// confirmation, a reason when the action needs one). Only shadcn components and theme tokens.

const STATUS_VARIANT: Record<WorkOrderStatus, 'default' | 'secondary' | 'outline' | 'destructive'> =
  {
    planned: 'outline',
    in_progress: 'default',
    on_hold: 'secondary',
    completed: 'secondary',
    delivered: 'outline',
    invoiced: 'outline',
    cancelled: 'destructive',
  };

export function OrderStatusBadge({ status }: { status: WorkOrderStatus }) {
  const { t } = useT();
  return <Badge variant={STATUS_VARIANT[status]}>{t(`yardOrders:status.${status}`)}</Badge>;
}

/**
 * Menu of the transitions of an order (yard.manage). The order is read when an action is chosen so that the
 * transition carries its current version (If-Match).
 */
export function OrderActions({
  orderId,
  status,
  deliveredAt,
  onDone,
}: {
  orderId: string;
  status: WorkOrderStatus;
  deliveredAt?: string | null;
  onDone?: (warnings: string[]) => void;
}) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [action, setAction] = useState<WorkOrderAction | null>(null);
  const order = useWorkOrder(marinaId, action ? orderId : undefined);
  const transition = useTransitionWorkOrder(marinaId);
  const actions = actionsFor(status, deliveredAt);
  if (!can('yard.manage') || actions.length === 0) return null;
  const reason = action ? needsReason(action) : null;
  const run = (text: string) => {
    if (!action || !order.data) return;
    transition.mutate(
      { id: orderId, version: order.data.version, action, reason: text || undefined },
      {
        onSuccess: (o) => {
          setAction(null);
          onDone?.(o.warnings);
        },
      },
    );
  };
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="outline"
            size="sm"
            className="min-h-11 sm:min-h-8"
            data-testid={`order-actions-${orderId}`}
          >
            {t('yardOrders:actions.label')}
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {actions.map((a) => (
            <DropdownMenuItem key={a} onSelect={() => setAction(a)}>
              {t(`yardOrders:actions.${a}`)}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <ReasonDialog
        open={action !== null}
        title={action ? t(`yardOrders:actions.${action}`) : ''}
        description={t('yardOrders:actions.confirmTitle')}
        confirmLabel={t('yardOrders:actions.confirm')}
        required={reason === 'required'}
        pending={transition.isPending || order.isPending}
        error={transition.error}
        destructive={action === 'cancel'}
        onClose={() => {
          setAction(null);
          transition.reset();
        }}
        onConfirm={run}
      />
    </>
  );
}
