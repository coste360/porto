import { getRouteApi, Link } from '@tanstack/react-router';
import { FileTextIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useEndRight,
  useRight,
  useRightHistory,
  useRightHistoryPdf,
  useSetRightVessels,
  useUpdateRight,
  type Right,
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
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, PageHeader } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  Detail,
  DetailGrid,
  IsoDate,
  M4ProblemAlert,
  PageFrame,
  Section,
} from '../contracts-m4/components/kit';
import { openBlob } from '../contracts-m4/lib/files';
import { VesselField } from '../desk/components/VesselField';
import { NO_VESSEL, type VesselValue } from '../desk/lib/vessel';
import { useMarinaToday } from '../registry/lib/today';
import { HoldersEditor } from './components/HoldersEditor';
import { rightVariant } from './lib/rights';
import { checkShares, holderRows, toHolderShares, type HolderDraft } from './lib/shares';

const route = getRouteApi('/auth/m/$marinaId/rights/$rightId');

/** One right: title, holders (editable), authorised vessels, holder chain, end or cancel. */
export function RightPage() {
  const { marinaId, can } = useCurrentMarina();
  const { rightId } = route.useParams();
  const right = useRight(marinaId, rightId);
  return (
    <RequirePermission allowed={can('rights.manage')}>
      <PageFrame>
        {right.isPending ? (
          <LoadingState rows={6} />
        ) : right.isError ? (
          <QueryErrorState error={right.error} onRetry={() => void right.refetch()} />
        ) : (
          <RightDetail right={right.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function RightDetail({ right }: { right: Right }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const pdf = useRightHistoryPdf(marinaId);
  const update = useUpdateRight(marinaId);
  const [dialog, setDialog] = useState<'end' | 'cancel' | null>(null);
  const [holders, setHolders] = useState<HolderDraft[] | null>(null);
  const code = right.allocations?.[0]?.resource_code ?? right.berth.resource_id;
  const active = right.status === 'active';
  return (
    <>
      <PageHeader
        title={t('rights:detail.heading', { code })}
        description={t(`docsEnums:rightKind.${right.kind}`)}
        actions={
          <>
            <Button
              variant="outline"
              disabled={pdf.isPending}
              onClick={() => pdf.mutate(right.berth.resource_id, { onSuccess: openBlob })}
            >
              <FileTextIcon />
              {t('rights:detail.chainPdf')}
            </Button>
            {active ? (
              <>
                <Button variant="outline" asChild>
                  <Link
                    to="/m/$marinaId/transfers"
                    params={{ marinaId }}
                    search={{ right: right.id }}
                  >
                    {t('rights:detail.transfer')}
                  </Link>
                </Button>
                <Button variant="outline" onClick={() => setDialog('end')}>
                  {t('rights:detail.end')}
                </Button>
                <Button variant="outline" onClick={() => setDialog('cancel')}>
                  {t('rights:detail.cancel')}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      <M4ProblemAlert error={pdf.error} />
      <div className="flex flex-wrap gap-2 text-sm">
        <Badge variant={rightVariant(right.status)}>
          {t(`docsEnums:rightStatus.${right.status}`)}
        </Badge>
        {right.unpaid_fees ? <Badge variant="destructive">{t('rights:list.unpaid')}</Badge> : null}
        {right.active_mandate_id ? (
          <Link
            to="/m/$marinaId/mandates/$mandateId"
            params={{ marinaId, mandateId: right.active_mandate_id }}
            className="underline-offset-4 hover:underline"
          >
            {t('rights:list.mandate')}
          </Link>
        ) : null}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Section title={t('rights:detail.title')}>
          <DetailGrid>
            <Detail label={t('rights:wizard.fromOn')}>
              <IsoDate date={right.from_on} />
            </Detail>
            <Detail label={t('rights:wizard.toOn')}>
              <IsoDate date={right.to_on} />
            </Detail>
            {(['deed', 'number', 'notary'] as const).map((k) =>
              right.title[k] ? (
                <Detail key={k} label={t(`rights:title.${k}`)}>
                  {right.title[k]}
                </Detail>
              ) : null,
            )}
            {right.title.date ? (
              <Detail label={t('rights:title.date')}>
                <IsoDate date={right.title.date} />
              </Detail>
            ) : null}
            {right.end_reason ? (
              <Detail label={t('contractsM4:common.reason')}>{right.end_reason}</Detail>
            ) : null}
          </DetailGrid>
        </Section>
        <Section
          title={t('rights:holders.title')}
          action={
            active && !holders ? (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setHolders(
                    right.holders.map((h) => ({
                      customer: { id: h.customer.id, label: h.customer.display_name ?? '' },
                      text: String(h.share_millis),
                    })),
                  )
                }
              >
                {t('common:actions.edit')}
              </Button>
            ) : undefined
          }
        >
          {holders ? (
            <>
              <HoldersEditor
                marinaId={marinaId}
                idPrefix="edit"
                rows={holders}
                onChange={setHolders}
                showErrors
              />
              <M4ProblemAlert error={update.error} />
              <div className="flex gap-2">
                <Button variant="outline" onClick={() => setHolders(null)}>
                  {t('common:actions.cancel')}
                </Button>
                <Button
                  disabled={!checkShares(holderRows(holders)).ok || update.isPending}
                  onClick={() =>
                    update.mutate(
                      {
                        id: right.id,
                        version: right.version,
                        patch: { holders: toHolderShares(holderRows(holders)) },
                      },
                      { onSuccess: () => setHolders(null) },
                    )
                  }
                >
                  {t('common:actions.save')}
                </Button>
              </div>
            </>
          ) : (
            <ul className="flex flex-col gap-1 text-sm">
              {right.holders.map((h) => (
                <li key={h.customer.id} className="flex justify-between gap-2">
                  <span>{h.customer.display_name ?? ''}</span>
                  <span className="tabular-nums">
                    {t('rights:holders.millis', { millis: h.share_millis })}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Section>
        <VesselsSection right={right} />
        <HistorySection right={right} />
      </div>
      {dialog ? (
        <EndDialog right={right} cancel={dialog === 'cancel'} onClose={() => setDialog(null)} />
      ) : null}
    </>
  );
}

function VesselsSection({ right }: { right: Right }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const set = useSetRightVessels(marinaId);
  const [vessel, setVessel] = useState<VesselValue>(NO_VESSEL);
  const active = right.status === 'active';
  const ids = right.vessels.map((v) => ({ vessel_id: v.vessel.id, primary: v.primary }));
  return (
    <Section title={t('rights:vessels.title')} description={t('rights:vessels.hint')}>
      <ul className="flex flex-col gap-2 text-sm">
        {right.vessels.map((v) => (
          <li key={v.vessel.id} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{v.vessel.name}</span>
            {v.primary ? <Badge>{t('rights:vessels.primary')}</Badge> : null}
            {v.fit && !v.fit.ok ? (
              <Badge variant="destructive">{t('rights:vessels.fitBlocked')}</Badge>
            ) : null}
            {active && !v.primary ? (
              <Button
                size="sm"
                variant="ghost"
                disabled={set.isPending}
                onClick={() =>
                  set.mutate({
                    id: right.id,
                    version: right.version,
                    body: {
                      vessels: ids.map((x) => ({ ...x, primary: x.vessel_id === v.vessel.id })),
                      from_night: today,
                    },
                  })
                }
              >
                {t('rights:vessels.makePrimary')}
              </Button>
            ) : null}
            {active ? (
              <Button
                size="sm"
                variant="ghost"
                disabled={set.isPending}
                onClick={() =>
                  set.mutate({
                    id: right.id,
                    version: right.version,
                    body: {
                      vessels: ids.filter((x) => x.vessel_id !== v.vessel.id),
                      from_night: today,
                    },
                  })
                }
              >
                {t('rights:vessels.remove')}
              </Button>
            ) : null}
          </li>
        ))}
      </ul>
      {active ? (
        <div className="flex flex-col gap-2">
          <VesselField
            marinaId={marinaId}
            idPrefix="right-add-vessel"
            value={vessel}
            onChange={setVessel}
          />
          <div>
            <Button
              size="sm"
              variant="outline"
              disabled={vessel.kind !== 'existing' || !vessel.id || set.isPending}
              onClick={() =>
                vessel.kind === 'existing' &&
                vessel.id &&
                set.mutate(
                  {
                    id: right.id,
                    version: right.version,
                    body: {
                      vessels: [...ids, { vessel_id: vessel.id, primary: ids.length === 0 }],
                      from_night: today,
                    },
                  },
                  { onSuccess: () => setVessel(NO_VESSEL) },
                )
              }
            >
              {t('rights:vessels.add')}
            </Button>
          </div>
        </div>
      ) : null}
      <M4ProblemAlert error={set.error} />
    </Section>
  );
}

function HistorySection({ right }: { right: Right }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const history = useRightHistory(marinaId, right.berth.resource_id);
  return (
    <Section title={t('rights:detail.chain')}>
      {history.isPending ? (
        <LoadingState rows={2} />
      ) : history.isError ? (
        <QueryErrorState error={history.error} />
      ) : (
        <ol className="flex flex-col gap-2 text-sm">
          {history.data.items.map((h) => (
            <li key={h.right_id} className="rounded-md border p-2">
              <div className="flex flex-wrap gap-2">
                <IsoDate date={h.from_on} />
                <IsoDate date={h.to_on} />
                <Badge variant="outline">{t(`docsEnums:rightStatus.${h.status}`)}</Badge>
              </div>
              <div className="text-muted-foreground">
                {h.holders
                  .map((x) => `${x.customer.display_name ?? ''} (${x.share_millis}‰)`)
                  .join(', ')}
              </div>
            </li>
          ))}
        </ol>
      )}
    </Section>
  );
}

function EndDialog({
  right,
  cancel,
  onClose,
}: {
  right: Right;
  cancel: boolean;
  onClose: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const today = useMarinaToday();
  const end = useEndRight(marinaId);
  const [on, setOn] = useState<string | null>(today);
  const [reason, setReason] = useState('');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{cancel ? t('rights:detail.cancel') : t('rights:detail.end')}</DialogTitle>
          <DialogDescription>
            {cancel ? t('rights:detail.cancelHint') : t('rights:detail.endHint')}
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="right-end-on">{t('rights:detail.endOn')}</FieldLabel>
          <DatePicker id="right-end-on" value={on} onChange={setOn} />
        </Field>
        <Field>
          <FieldLabel htmlFor="right-end-reason">{t('contractsM4:common.reason')}</FieldLabel>
          <Textarea
            id="right-end-reason"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
        <M4ProblemAlert error={end.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button
            variant="destructive"
            disabled={!on || !reason.trim() || end.isPending}
            onClick={() =>
              on &&
              end.mutate(
                {
                  id: right.id,
                  version: right.version,
                  cancel,
                  body: { on, reason: reason.trim() },
                },
                { onSuccess: onClose },
              )
            }
          >
            {cancel ? t('rights:detail.cancel') : t('rights:detail.end')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
