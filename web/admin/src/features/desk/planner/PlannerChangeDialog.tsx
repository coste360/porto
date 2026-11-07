import {
  useChangeTransitStay,
  useContract,
  useMoveAllocation,
  type MapAllocation,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';

import { DeskProblemAlert } from '../components/DeskProblemAlert';

export type PlannerChange =
  | { kind: 'move'; allocation: MapAllocation; move: Schemas['MoveRequest']; targetCode: string }
  | {
      kind: 'stay';
      allocation: MapAllocation;
      contractId: string;
      stay: { arrival?: string; departure: string };
    };

/** Confirmation of a planner drag: a move to another place, or a stay change of a transit (with its version). */
export function PlannerChangeDialog({
  marinaId,
  change,
  onClose,
}: {
  marinaId: string;
  change: PlannerChange;
  onClose: () => void;
}) {
  const { t } = useT();
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="desk-planner-change">
        {change.kind === 'move' ? (
          <MoveConfirm marinaId={marinaId} change={change} onClose={onClose} />
        ) : (
          <StayConfirm marinaId={marinaId} change={change} onClose={onClose} />
        )}
        <span className="sr-only">{t('desk:planner.title')}</span>
      </DialogContent>
    </Dialog>
  );
}

function MoveConfirm({
  marinaId,
  change,
  onClose,
}: {
  marinaId: string;
  change: Extract<PlannerChange, { kind: 'move' }>;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const move = useMoveAllocation(marinaId);
  return (
    <>
      <DialogHeader>
        <DialogTitle>{t('desk:planner.moveTitle')}</DialogTitle>
        <DialogDescription>
          {t('desk:planner.moveDescription', {
            vessel: change.allocation.vessel?.name ?? '',
            code: change.targetCode,
            from: formatIsoDate(change.move.from_night ?? change.allocation.first_night),
          })}
        </DialogDescription>
      </DialogHeader>
      {move.data?.price_effect ? (
        <p className="text-sm">
          {t('desk:transit.priceEffect', {
            amount: formatCents(move.data.price_effect.delta_cents, 'EUR', i18n.language),
          })}
        </p>
      ) : null}
      <DeskProblemAlert error={move.error} />
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button
          disabled={move.isPending}
          onClick={() => move.mutate({ body: change.move }, { onSuccess: onClose })}
        >
          {t('desk:planner.moveSubmit')}
        </Button>
      </DialogFooter>
    </>
  );
}

function StayConfirm({
  marinaId,
  change,
  onClose,
}: {
  marinaId: string;
  change: Extract<PlannerChange, { kind: 'stay' }>;
  onClose: () => void;
}) {
  const { t } = useT();
  const contract = useContract(marinaId, change.contractId);
  const stay = useChangeTransitStay(marinaId);
  const arrival = change.stay.arrival ?? change.allocation.first_night;
  return (
    <>
      <DialogHeader>
        <DialogTitle>{t('desk:transit.stayTitle')}</DialogTitle>
        <DialogDescription>
          {t('desk:planner.stayDescription', {
            vessel: change.allocation.vessel?.name ?? '',
            from: formatIsoDate(arrival),
            to: formatIsoDate(change.stay.departure),
          })}
        </DialogDescription>
      </DialogHeader>
      <DeskProblemAlert error={contract.error ?? stay.error} />
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button
          disabled={!contract.data || stay.isPending}
          onClick={() =>
            contract.data &&
            stay.mutate(
              {
                id: contract.data.id,
                version: contract.data.version,
                body: {
                  ...(change.stay.arrival ? { arrival: change.stay.arrival } : {}),
                  departure: change.stay.departure,
                },
              },
              { onSuccess: onClose },
            )
          }
        >
          {t('common:actions.confirm')}
        </Button>
      </DialogFooter>
    </>
  );
}
