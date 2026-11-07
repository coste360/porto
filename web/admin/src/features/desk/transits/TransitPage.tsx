import { getRouteApi } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useCancelTransit,
  useChangeTransitStay,
  useConfirmTransit,
  useContract,
  useMarkTransitNoShow,
  useUpdateTransit,
  type Contract,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatDateTime, formatIsoDate } from '@porto/shared/lib/format';
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
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  ContractChargesCard,
  DeskPaymentFields,
  TransitPaymentLinkButton,
  defaultDeskPayment,
  deskPaymentBody,
} from '../../billing/public';
import { useMarinaToday } from '../../registry/lib/today';
import {
  AllocationsCard,
  LinesCard,
  PresencesCard,
  ShowOnMapButton,
} from '../components/ContractSections';
import { CustomerField, type CustomerChoice } from '../components/CustomerField';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { StatusBadge } from '../components/StatusBadge';
import { daysBetween, isIsoDate } from '../lib/dates';
import { transitActions } from '../lib/status';
import { ManualConfirmFields } from './NewTransitDialog';

const route = getRouteApi('/auth/m/$marinaId/transits/$contractId');

export function TransitPage() {
  const { marinaId, can } = useCurrentMarina();
  const { contractId } = route.useParams();
  const transit = useContract(marinaId, contractId);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      {transit.isPending ? (
        <LoadingState rows={6} />
      ) : transit.isError ? (
        <QueryErrorState error={transit.error} onRetry={() => void transit.refetch()} />
      ) : (
        <TransitDetail transit={transit.data} />
      )}
    </RequirePermission>
  );
}

function TransitDetail({ transit }: { transit: Contract }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const noShow = useMarkTransitNoShow(marinaId);
  const [dialog, setDialog] = useState<'confirm' | 'cancel' | 'stay' | 'customer' | null>(null);
  const desk = can('transits.desk');
  const actions = transitActions(transit.status, today, transit.starts_on);
  return (
    <>
      <PageHeader
        title={transit.code}
        description={`${transit.vessel.name}${transit.customer?.display_name ? ` · ${transit.customer.display_name}` : ''}`}
        actions={
          <>
            <ShowOnMapButton contract={transit} date={today} />
            <TransitPaymentLinkButton transit={transit} />
            {desk && actions.confirm ? (
              <Button onClick={() => setDialog('confirm')}>{t('desk:transit.confirm')}</Button>
            ) : null}
            {desk && actions.stay ? (
              <Button variant="outline" onClick={() => setDialog('stay')}>
                {t('desk:transit.changeStay')}
              </Button>
            ) : null}
            {desk && can('customers.read') ? (
              <Button variant="outline" onClick={() => setDialog('customer')}>
                {t('desk:transit.linkCustomer')}
              </Button>
            ) : null}
            {desk && actions.noShow ? (
              <Button
                variant="outline"
                disabled={noShow.isPending}
                onClick={() => noShow.mutate({ id: transit.id, version: transit.version })}
              >
                {t('desk:transit.noShow')}
              </Button>
            ) : null}
            {desk && actions.cancel ? (
              <Button variant="outline" onClick={() => setDialog('cancel')}>
                {t('desk:transit.cancel')}
              </Button>
            ) : null}
          </>
        }
      />
      <DeskProblemAlert error={noShow.error} />
      <div className="mb-4 flex flex-wrap items-center gap-3 text-sm">
        <StatusBadge kind="transit" status={transit.status} />
        <span>
          {t('desk:stayRange', {
            from: formatIsoDate(transit.starts_on),
            to: formatIsoDate(transit.ends_on),
            count: daysBetween(transit.starts_on, transit.ends_on),
          })}
        </span>
        {transit.status === 'held' && transit.hold_expires_at ? (
          <span data-testid="desk-transit-hold">
            {t('desk:transit.heldUntil', {
              at: formatDateTime(transit.hold_expires_at, i18n.language),
            })}
          </span>
        ) : null}
        {transit.confirmation_source ? (
          <span>
            {t('desk:transit.confirmedBy', {
              source: t(`occupancyEnums:confirmationSource.${transit.confirmation_source}`),
            })}
          </span>
        ) : null}
        {transit.pay_at_departure ? <span>{t('desk:transit.payAtDeparture')}</span> : null}
        <span className="tabular-nums">
          {formatCents(transit.total_cents, 'EUR', i18n.language)}
        </span>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <LinesCard contract={transit} />
        <AllocationsCard contract={transit} />
        <PresencesCard contract={transit} />
      </div>
      <div className="mt-4">
        <ContractChargesCard contractId={transit.id} customer={transit.customer ?? null} />
      </div>
      {dialog === 'confirm' ? (
        <ConfirmDialog transit={transit} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'cancel' ? (
        <CancelTransitDialog transit={transit} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'stay' ? (
        <StayDialog
          transit={transit}
          arrivalEditable={actions.arrivalEditable}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {dialog === 'customer' ? (
        <CustomerDialog transit={transit} onClose={() => setDialog(null)} />
      ) : null}
    </>
  );
}

/** Manual driver (INV-09): confirmation with a free-text reference and "pay at departure". */
function ConfirmDialog({ transit, onClose }: { transit: Contract; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const confirm = useConfirmTransit(marinaId);
  const [value, setValue] = useState({ reference: '', payAtDeparture: false });
  const [payment, setPayment] = useState(() => defaultDeskPayment(transit.total_cents));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="desk-confirm-dialog">
        <DialogHeader>
          <DialogTitle>{t('desk:transit.confirmTitle')}</DialogTitle>
          <DialogDescription>{t('desk:transit.confirmDescription')}</DialogDescription>
        </DialogHeader>
        {!transit.customer ? (
          <p className="text-destructive text-sm">{t('errors:customer_required')}</p>
        ) : null}
        <ManualConfirmFields idPrefix="confirm" value={value} onChange={setValue} />
        <DeskPaymentFields idPrefix="confirm" value={payment} onChange={setPayment} />
        <DeskProblemAlert error={confirm.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={confirm.isPending || !transit.customer}
            onClick={() =>
              confirm.mutate(
                {
                  id: transit.id,
                  version: transit.version,
                  body: {
                    pay_at_departure: value.payAtDeparture,
                    ...(value.reference.trim() ? { reference: value.reference.trim() } : {}),
                    ...deskPaymentBody(payment),
                  },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('desk:transit.submitConfirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CancelTransitDialog({ transit, onClose }: { transit: Contract; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const cancel = useCancelTransit(marinaId);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:transit.cancelTitle')}</DialogTitle>
          <DialogDescription>{t('desk:transit.cancelDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="transit-cancel-reason">{t('desk:fields.reason')}</FieldLabel>
          <Input
            id="transit-cancel-reason"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <DeskProblemAlert error={cancel.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            variant="destructive"
            disabled={cancel.isPending}
            onClick={() =>
              cancel.mutate(
                {
                  id: transit.id,
                  version: transit.version,
                  ...(reason.trim() ? { reason: reason.trim() } : {}),
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('desk:transit.cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Stay change (13.5): added nights must be free; the alternatives of a conflict become extension targets. */
function StayDialog({
  transit,
  arrivalEditable,
  onClose,
}: {
  transit: Contract;
  arrivalEditable: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const stay = useChangeTransitStay(marinaId);
  const [arrival, setArrival] = useState<string | null>(transit.starts_on);
  const [departure, setDeparture] = useState<string | null>(transit.ends_on);
  const valid = isIsoDate(arrival) && isIsoDate(departure) && arrival < departure;
  const run = (
    extra: {
      extension_target?: {
        resource_type: 'berth' | 'dynamic_area' | 'land_resource';
        resource_id: string;
        x_cm?: number;
      };
      fit_override?: { reason: string };
    } = {},
  ) => {
    if (!valid) return;
    stay.mutate(
      {
        id: transit.id,
        version: transit.version,
        body: {
          ...(arrivalEditable && arrival !== transit.starts_on ? { arrival } : {}),
          ...(departure !== transit.ends_on ? { departure } : {}),
          ...extra,
        },
      },
      { onSuccess: onClose },
    );
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid="desk-stay-dialog">
        <DialogHeader>
          <DialogTitle>{t('desk:transit.stayTitle')}</DialogTitle>
          <DialogDescription>{t('desk:transit.stayDescription')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="stay-arrival">{t('desk:fields.arrival')}</FieldLabel>
            <DatePicker
              id="stay-arrival"
              value={arrival}
              onChange={setArrival}
              disabled={!arrivalEditable}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="stay-departure">{t('desk:fields.departure')}</FieldLabel>
            <DatePicker id="stay-departure" value={departure} onChange={setDeparture} />
          </Field>
        </div>
        {stay.data?.price_effect ? (
          <p className="text-sm">
            {t('desk:transit.priceEffect', {
              amount: formatCents(stay.data.price_effect.delta_cents, 'EUR', i18n.language),
            })}
          </p>
        ) : null}
        <DeskProblemAlert
          error={stay.error}
          onAlternative={(s) => {
            const seg = s.segments[0];
            if (seg)
              run({
                extension_target: {
                  resource_type: seg.resource_type,
                  resource_id: seg.resource_id,
                  ...(seg.x_cm !== undefined ? { x_cm: seg.x_cm } : {}),
                },
              });
          }}
          {...(can('occupancy.override_fit')
            ? { onOverride: (r: string) => run({ fit_override: { reason: r } }) }
            : {})}
        />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button disabled={!valid || stay.isPending} onClick={() => run()}>
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CustomerDialog({ transit, onClose }: { transit: Contract; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateTransit(marinaId);
  const [customer, setCustomer] = useState<CustomerChoice>(
    transit.customer
      ? { id: transit.customer.id, label: transit.customer.display_name ?? '' }
      : null,
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:transit.linkCustomer')}</DialogTitle>
          <DialogDescription>{t('desk:transit.linkCustomerDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="transit-link-customer">{t('desk:fields.customer')}</FieldLabel>
          <CustomerField
            marinaId={marinaId}
            id="transit-link-customer"
            value={customer}
            onChange={setCustomer}
          />
        </Field>
        <DeskProblemAlert error={update.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={update.isPending}
            onClick={() =>
              update.mutate(
                {
                  id: transit.id,
                  version: transit.version,
                  patch: { customer_id: customer?.id ?? null },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('common:actions.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
