import { ZapIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useAssignUtilityConsumption,
  useContracts,
  useExcludeUtilityConsumption,
  useRunUtilityCharges,
  useUtilityConsumptions,
  type UtilityChargeRunResult,
  type UtilityConsumption,
  type UtilityConsumptionStatus,
} from '@porto/shared/api';
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
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { ElementCombobox, PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { SimpleTable } from '../../billing/components/kit';
import {
  EnumSelect,
  IsoDate,
  M4ProblemAlert,
  Money,
  PageFrame,
  ReasonDialog,
} from '../../contracts-m4/components/kit';
import { ConsumptionStatusBadge, Qty, UtilitiesNav } from '../components/kit';
import { UNIT_OF } from '../lib';

const STATUSES: readonly UtilityConsumptionStatus[] = [
  'pending',
  'anomaly',
  'unassigned',
  'charged',
  'excluded',
];

/** Consumptions review (m9-plan §9.3, §9.4): pending, anomalies, unassigned (assign or exclude) and the
 * "Addebita consumi" run with its summary. */
export function ConsumptionsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const [status, setStatus] = useState<UtilityConsumptionStatus | ''>('pending');
  const list = useUtilityConsumptions(marinaId, status ? { status } : {});
  const exclude = useExcludeUtilityConsumption(marinaId);
  const run = useRunUtilityCharges(marinaId);
  const [excluding, setExcluding] = useState<UtilityConsumption | null>(null);
  const [assigning, setAssigning] = useState<UtilityConsumption | null>(null);
  const [result, setResult] = useState<UtilityChargeRunResult | null>(null);
  const items = list.data?.items ?? [];
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('utilities:consumptions.title')}
          description={t('utilities:consumptions.description')}
          actions={
            <Button
              disabled={run.isPending}
              onClick={() => run.mutate(undefined, { onSuccess: setResult })}
            >
              <ZapIcon />
              {t('utilities:consumptions.run')}
            </Button>
          }
        />
        <UtilitiesNav marinaId={marinaId} current="consumptions" />
        <EnumSelect
          id="consumption-status"
          label={t('utilities:fields.status')}
          values={STATUSES}
          value={status}
          onChange={setStatus}
          labelOf={(s) => t(`utilities:enums.consumptionStatus.${s}`)}
          allLabel={t('utilities:all')}
        />
        <M4ProblemAlert error={run.error} />
        {result ? <RunSummary result={result} /> : null}
        {list.isPending ? (
          <LoadingState rows={5} />
        ) : list.isError ? (
          <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
        ) : (
          <SimpleTable
            head={[
              t('utilities:fields.meter'),
              t('utilities:fields.contract'),
              t('utilities:fields.nights'),
              t('utilities:fields.quantity'),
              t('utilities:fields.status'),
              '',
            ]}
            empty={t('utilities:consumptions.empty')}
            rows={items.length}
          >
            {items.map((c) => (
              <TableRow key={c.id} data-testid="consumption-row">
                <TableCell>{c.meter_code ?? ''}</TableCell>
                <TableCell>{c.contract?.code ?? ''}</TableCell>
                <TableCell>
                  <IsoDate date={c.first_night} /> – <IsoDate date={c.last_night} />
                </TableCell>
                <TableCell>
                  <Qty milli={c.qty_milli} unit={UNIT_OF[c.kind]} />
                </TableCell>
                <TableCell>
                  <ConsumptionStatusBadge status={c.status} />
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-2">
                    {c.status === 'unassigned' ? (
                      <Button size="sm" variant="outline" onClick={() => setAssigning(c)}>
                        {t('utilities:consumptions.assign')}
                      </Button>
                    ) : null}
                    {c.status !== 'charged' && c.status !== 'excluded' ? (
                      <Button size="sm" variant="outline" onClick={() => setExcluding(c)}>
                        {t('utilities:consumptions.exclude')}
                      </Button>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </SimpleTable>
        )}
        <ReasonDialog
          open={!!excluding}
          title={t('utilities:consumptions.exclude')}
          confirmLabel={t('utilities:consumptions.exclude')}
          required
          pending={exclude.isPending}
          error={exclude.error}
          onClose={() => setExcluding(null)}
          onConfirm={(reason) =>
            excluding &&
            exclude.mutate(
              { id: excluding.id, version: excluding.version, reason },
              { onSuccess: () => setExcluding(null) },
            )
          }
        />
        {assigning ? (
          <AssignDialog
            marinaId={marinaId}
            consumption={assigning}
            onClose={() => setAssigning(null)}
          />
        ) : null}
      </PageFrame>
    </RequirePermission>
  );
}

export function RunSummary({ result }: { result: UtilityChargeRunResult }) {
  const { t } = useT();
  const total = result.charges.reduce((s, c) => s + c.total_cents, 0);
  return (
    <div role="status" className="rounded-md border p-3 text-sm" data-testid="utility-run-summary">
      <p className="font-medium">
        {t('utilities:consumptions.runCharges', { count: result.charges.length })}{' '}
        <Money cents={total} />
      </p>
      {result.skipped.length > 0 ? (
        <p className="text-muted-foreground">
          {t('utilities:consumptions.runSkipped', { count: result.skipped.length })}
        </p>
      ) : null}
    </div>
  );
}

function AssignDialog({
  marinaId,
  consumption,
  onClose,
}: {
  marinaId: string;
  consumption: UtilityConsumption;
  onClose: () => void;
}) {
  const { t } = useT();
  const assign = useAssignUtilityConsumption(marinaId);
  const [q, setQ] = useState('');
  const contracts = useContracts(marinaId, q ? { q } : {});
  const [contractId, setContractId] = useState<string | null>(null);
  const options = (contracts.data?.pages.flatMap((p) => p.items) ?? []).map((c) => ({
    value: c.id,
    label: c.code,
  }));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('utilities:consumptions.assign')}</DialogTitle>
          <DialogDescription>{t('utilities:consumptions.assignHint')}</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="assign-contract">{t('utilities:fields.contract')}</FieldLabel>
          <ElementCombobox
            id="assign-contract"
            options={options}
            value={contractId}
            onChange={setContractId}
            onSearch={setQ}
            placeholder={t('utilities:consumptions.chooseContract')}
            searchPlaceholder={t('utilities:meters.search')}
            emptyText={t('utilities:consumptions.noContract')}
          />
        </Field>
        <M4ProblemAlert error={assign.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={!contractId || assign.isPending}
            onClick={() =>
              assign.mutate(
                { id: consumption.id, version: consumption.version, contractId: contractId! },
                { onSuccess: onClose },
              )
            }
          >
            {t('utilities:consumptions.assign')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
