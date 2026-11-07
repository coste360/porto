import { useState } from 'react';

import {
  useActivateAddendum,
  useAddenda,
  useCancelAddendum,
  useCreateAddendum,
  useRequestAddendumSignature,
  type Addendum,
  type Contract,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
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
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, MoneyInput } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { RequestSignatureDialog, SignaturePanel } from '../../documents/public';
import { CustomerField } from '../../desk/components/CustomerField';
import { VesselField } from '../../desk/components/VesselField';
import { NO_VESSEL, type VesselValue } from '../../desk/lib/vessel';
import { useMarinaToday } from '../../registry/lib/today';
import { EnumSelect, IsoDate, M4ProblemAlert, Money } from './kit';
import { ADDENDUM_KINDS, addendumBody, type AddendumDraft } from '../lib/addenda';

function addendumVariant(s: Addendum['status']) {
  return s === 'active' ? 'default' : s === 'cancelled' ? 'destructive' : 'secondary';
}

/** Addenda of a contract (13.5): list, create, signature, activation and cancel. */
export function AddendaPanel({ contract }: { contract: Contract }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const addenda = useAddenda(marinaId, contract.id);
  const [creating, setCreating] = useState(false);
  const [signing, setSigning] = useState<Addendum | null>(null);
  const request = useRequestAddendumSignature(marinaId);
  const activate = useActivateAddendum(marinaId);
  const cancel = useCancelAddendum(marinaId);
  const write = can('contracts.write');
  if (addenda.isPending) return <LoadingState rows={2} />;
  if (addenda.isError)
    return <QueryErrorState error={addenda.error} onRetry={() => void addenda.refetch()} />;
  return (
    <div className="flex flex-col gap-3">
      {write && ['draft', 'awaiting_signature', 'active', 'suspended'].includes(contract.status) ? (
        <div>
          <Button onClick={() => setCreating(true)}>{t('contractsM4:addenda.new')}</Button>
        </div>
      ) : null}
      <M4ProblemAlert error={activate.error ?? cancel.error} />
      {addenda.data.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('contractsM4:addenda.empty')}</p>
      ) : (
        <ol className="flex flex-col gap-3">
          {addenda.data.map((a) => {
            const ref = { contractId: contract.id, addendumId: a.id, version: a.version };
            return (
              <li key={a.id} className="flex flex-col gap-2 rounded-md border p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">
                    {t('contractsM4:addenda.seq', { seq: a.seq })}
                  </span>
                  <span>{t(`docsEnums:addendumKind.${a.kind}`)}</span>
                  <Badge variant={addendumVariant(a.status)}>
                    {t(`docsEnums:addendumStatus.${a.status}`)}
                  </Badge>
                  <span className="text-muted-foreground text-sm">
                    {t('contractsM4:addenda.effective')} <IsoDate date={a.effective_on} />
                  </span>
                  {a.amount_delta_cents !== 0 ? (
                    <span className="text-sm">
                      {t('contractsM4:addenda.delta')} <Money cents={a.amount_delta_cents} />
                    </span>
                  ) : null}
                </div>
                {a.signature_request_id ? (
                  <SignaturePanel requestId={a.signature_request_id} />
                ) : null}
                {write && a.status === 'draft' ? (
                  <div className="flex flex-wrap gap-2">
                    <Button size="sm" variant="outline" onClick={() => setSigning(a)}>
                      {t('contractsM4:contract.requestSignature')}
                    </Button>
                    <Button
                      size="sm"
                      disabled={activate.isPending}
                      onClick={() => activate.mutate(ref)}
                    >
                      {t('contractsM4:addenda.activatePaper')}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={cancel.isPending}
                      onClick={() => cancel.mutate(ref)}
                    >
                      {t('contractsM4:addenda.cancel')}
                    </Button>
                  </div>
                ) : null}
              </li>
            );
          })}
        </ol>
      )}
      {creating ? (
        <NewAddendumDialog contract={contract} onClose={() => setCreating(false)} />
      ) : null}
      <RequestSignatureDialog
        open={signing !== null}
        pending={request.isPending}
        error={request.error}
        onClose={() => setSigning(null)}
        onSubmit={(body) =>
          signing &&
          request.mutate(
            { contractId: contract.id, addendumId: signing.id, version: signing.version, body },
            { onSuccess: () => setSigning(null) },
          )
        }
      />
    </div>
  );
}

function NewAddendumDialog({ contract, onClose }: { contract: Contract; onClose: () => void }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const create = useCreateAddendum(marinaId);
  const [vessel, setVessel] = useState<VesselValue>(NO_VESSEL);
  const [d, setD] = useState<AddendumDraft>({
    kind: '',
    // Today, or the contract start when it has not started yet (the addendum must fall inside the contract).
    effectiveOn: contract.starts_on > today ? contract.starts_on : today,
    itemId: '',
    lineId: '',
    landResourceId: '',
    customer: null,
    vesselId: '',
    newTotalCents: null,
    reason: '',
  });
  const [touched, setTouched] = useState(false);
  const mapped = addendumBody({
    ...d,
    vesselId: vessel.kind === 'existing' ? (vessel.id ?? '') : '',
  });
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('contractsM4:addenda.newTitle')}</DialogTitle>
          <DialogDescription>{t('contractsM4:addenda.newDescription')}</DialogDescription>
        </DialogHeader>
        <EnumSelect
          id="addendum-kind"
          label={t('contractsM4:addenda.kind')}
          group="addendumKind"
          values={ADDENDUM_KINDS}
          value={d.kind}
          onChange={(kind) => setD({ ...d, kind })}
        />
        <Field>
          <FieldLabel htmlFor="addendum-effective">{t('contractsM4:addenda.effective')}</FieldLabel>
          <DatePicker
            id="addendum-effective"
            value={d.effectiveOn}
            onChange={(effectiveOn) => setD({ ...d, effectiveOn })}
          />
        </Field>
        {d.kind === 'service_add' ? (
          <Field>
            <FieldLabel htmlFor="addendum-item">{t('contractsM4:addenda.item')}</FieldLabel>
            <Input
              id="addendum-item"
              value={d.itemId}
              onChange={(e) => setD({ ...d, itemId: e.target.value })}
            />
          </Field>
        ) : null}
        {d.kind === 'service_remove' ? (
          <EnumSelect
            id="addendum-line"
            label={t('contractsM4:addenda.line')}
            values={contract.lines.map((l) => l.id)}
            labelOf={(id) => {
              const l = contract.lines.find((x) => x.id === id);
              return l ? `${l.seq}. ${l.resource_code ?? l.item_id ?? l.line_kind}` : id;
            }}
            value={d.lineId}
            onChange={(lineId) => setD({ ...d, lineId })}
          />
        ) : null}
        {d.kind === 'parking' ? (
          <Field>
            <FieldLabel htmlFor="addendum-land">{t('contractsM4:addenda.land')}</FieldLabel>
            <Input
              id="addendum-land"
              value={d.landResourceId}
              onChange={(e) => setD({ ...d, landResourceId: e.target.value })}
            />
          </Field>
        ) : null}
        {d.kind === 'billing_customer' ? (
          <Field>
            <FieldLabel htmlFor="addendum-customer">{t('contractsM4:addenda.customer')}</FieldLabel>
            <CustomerField
              marinaId={marinaId}
              id="addendum-customer"
              value={d.customer}
              onChange={(customer) => setD({ ...d, customer })}
            />
          </Field>
        ) : null}
        {d.kind === 'vessel_change' ? (
          <VesselField
            marinaId={marinaId}
            idPrefix="addendum-vessel"
            value={vessel}
            onChange={setVessel}
          />
        ) : null}
        {d.kind === 'price_change' ? (
          <Field>
            <FieldLabel htmlFor="addendum-total">{t('contractsM4:addenda.newTotal')}</FieldLabel>
            <MoneyInput
              id="addendum-total"
              locale={i18n.language}
              value={d.newTotalCents}
              onChange={(newTotalCents) => setD({ ...d, newTotalCents })}
            />
          </Field>
        ) : null}
        <Field>
          <FieldLabel htmlFor="addendum-reason">{t('contractsM4:common.reason')}</FieldLabel>
          <Input
            id="addendum-reason"
            value={d.reason}
            maxLength={500}
            onChange={(e) => setD({ ...d, reason: e.target.value })}
          />
        </Field>
        {touched && !mapped.ok ? (
          <FieldError>{t(`contractsM4:addenda.errors.${mapped.error}`)}</FieldError>
        ) : null}
        <M4ProblemAlert error={create.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            disabled={create.isPending}
            onClick={() => {
              setTouched(true);
              if (mapped.ok)
                create.mutate(
                  { contractId: contract.id, body: mapped.body },
                  { onSuccess: onClose },
                );
            }}
          >
            {t('contractsM4:addenda.create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
