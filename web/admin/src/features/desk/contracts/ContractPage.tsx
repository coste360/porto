import { getRouteApi } from '@tanstack/react-router';
import { useState } from 'react';

import {
  useActivateContract,
  useCancelContract,
  useContract,
  useTerminateContract,
  type Contract,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime, formatIsoDate } from '@porto/shared/lib/format';
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
import { Input } from '@porto/shared/ui/input';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import {
  ContractM4Actions,
  ContractM4Badges,
  ContractM4Tabs,
  type ContractTab,
} from '../../contracts-m4/public';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { useMarinaToday } from '../../registry/lib/today';
import { StorageContractSection } from '../../storage/public';
import {
  AllocationsCard,
  LinesCard,
  PresencesCard,
  ReleasesCard,
  ShowOnMapButton,
} from '../components/ContractSections';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { StatusBadge } from '../components/StatusBadge';
import { ReleaseDialog } from '../dialogs/ReleaseDialog';
import { contractActions } from '../lib/status';

const route = getRouteApi('/auth/m/$marinaId/contracts/$contractId');

export function ContractPage() {
  const { marinaId, can } = useCurrentMarina();
  const { contractId } = route.useParams();
  const contract = useContract(marinaId, contractId);
  return (
    <RequirePermission allowed={can('contracts.read')}>
      {contract.isPending ? (
        <LoadingState rows={6} />
      ) : contract.isError ? (
        <QueryErrorState error={contract.error} onRetry={() => void contract.refetch()} />
      ) : (
        <ContractDetail contract={contract.data} />
      )}
    </RequirePermission>
  );
}

function ContractDetail({ contract }: { contract: Contract }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const today = useMarinaToday();
  const activate = useActivateContract(marinaId);
  const [dialog, setDialog] = useState<'cancel' | 'terminate' | 'release' | null>(null);
  const [tab, setTab] = useState<ContractTab>('data');
  const m3 = contractActions(contract.status);
  // M4 (m4-plan §3.1): paper activation also from awaiting_signature, cancel from awaiting_signature, terminate
  // from suspended.
  const actions = {
    activate: m3.activate || contract.status === 'awaiting_signature',
    cancel: m3.cancel || contract.status === 'awaiting_signature',
    terminate: m3.terminate || contract.status === 'suspended',
  };
  const write = can('contracts.write');
  const releasable = contract.allocations.find(
    (a) => a.status === 'active' && a.kind === 'contract' && a.last_night >= today,
  );
  return (
    <>
      <PageHeader
        title={contract.code}
        description={`${contract.vessel.name}${contract.customer?.display_name ? ` · ${contract.customer.display_name}` : ''}`}
        actions={
          <>
            <ShowOnMapButton contract={contract} date={today} />
            {write && actions.activate ? (
              <Button
                disabled={activate.isPending}
                onClick={() => activate.mutate({ id: contract.id, version: contract.version })}
              >
                {t('desk:contract.activate')}
              </Button>
            ) : null}
            {write && actions.cancel ? (
              <Button variant="outline" onClick={() => setDialog('cancel')}>
                {t('desk:contract.cancel')}
              </Button>
            ) : null}
            {write && actions.terminate ? (
              <Button variant="outline" onClick={() => setDialog('terminate')}>
                {t('desk:contract.terminate')}
              </Button>
            ) : null}
            <ContractM4Actions contract={contract} />
            {releasable && can('occupancy.move') ? (
              <Button variant="outline" onClick={() => setDialog('release')}>
                {t('desk:release.open')}
              </Button>
            ) : null}
          </>
        }
      />
      <DeskProblemAlert error={activate.error} />
      <div className="mb-4 flex flex-wrap items-center gap-3 text-sm">
        <StatusBadge kind={contract.kind} status={contract.status} />
        {contract.term ? <span>{t(`occupancyEnums:contractTerm.${contract.term}`)}</span> : null}
        <span>{`${formatIsoDate(contract.starts_on)} – ${formatIsoDate(contract.ends_on)}`}</span>
        {contract.hold_lost ? (
          <Badge variant="destructive">{t('desk:contract.holdLost')}</Badge>
        ) : null}
        {contract.status === 'draft' && contract.hold_expires_at ? (
          <span>
            {t('desk:contract.holdUntil', {
              at: formatDateTime(contract.hold_expires_at, i18n.language),
            })}
          </span>
        ) : null}
        <ContractM4Badges contract={contract} />
        {contract.terminated_on ? (
          <span>
            {t('desk:contract.terminatedOn', { date: formatIsoDate(contract.terminated_on) })}
          </span>
        ) : null}
      </div>
      <ContractM4Tabs
        contract={contract}
        tab={tab}
        onTab={setTab}
        data={
          <div className="grid gap-4 lg:grid-cols-2">
            <LinesCard contract={contract} />
            <AllocationsCard contract={contract} />
            <ReleasesCard contract={contract} canRevoke={can('occupancy.move')} />
            <PresencesCard contract={contract} />
            <StorageContractSection contract={contract} />
          </div>
        }
      />
      {dialog === 'cancel' ? (
        <CancelDialog contract={contract} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'terminate' ? (
        <TerminateDialog contract={contract} today={today} onClose={() => setDialog(null)} />
      ) : null}
      {dialog === 'release' && releasable ? (
        <ReleaseDialog
          marinaId={marinaId}
          open
          onOpenChange={(o) => !o && setDialog(null)}
          allocationId={releasable.id}
          defaultFrom={releasable.first_night > today ? releasable.first_night : today}
        />
      ) : null}
    </>
  );
}

function CancelDialog({ contract, onClose }: { contract: Contract; onClose: () => void }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const cancel = useCancelContract(marinaId);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:contract.cancelTitle')}</DialogTitle>
          <DialogDescription>{t('desk:contract.cancelDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="cancel-reason">{t('desk:fields.reason')}</FieldLabel>
          <Input
            id="cancel-reason"
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
                  id: contract.id,
                  version: contract.version,
                  ...(reason.trim() ? { reason: reason.trim() } : {}),
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('desk:contract.cancel')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function TerminateDialog({
  contract,
  today,
  onClose,
}: {
  contract: Contract;
  today: string;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const terminate = useTerminateContract(marinaId);
  const [effective, setEffective] = useState<string | null>(today);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desk:contract.terminateTitle')}</DialogTitle>
          <DialogDescription>{t('desk:contract.terminateDescription')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="terminate-on">{t('desk:contract.effectiveOn')}</FieldLabel>
          <DatePicker id="terminate-on" value={effective} onChange={setEffective} />
        </Field>
        <Field>
          <FieldLabel htmlFor="terminate-reason">{t('desk:fields.reason')}</FieldLabel>
          <Input
            id="terminate-reason"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <DeskProblemAlert error={terminate.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            variant="destructive"
            disabled={!effective || !reason.trim() || terminate.isPending}
            onClick={() =>
              effective &&
              terminate.mutate(
                {
                  id: contract.id,
                  version: contract.version,
                  body: { effective_on: effective, reason: reason.trim() },
                },
                { onSuccess: onClose },
              )
            }
          >
            {t('desk:contract.terminate')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
