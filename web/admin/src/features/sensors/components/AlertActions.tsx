import { useState } from 'react';

import {
  useActOnSensorAlert,
  type SensorAlert,
  type SensorAlertAction,
  type SensorAlertActionInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { ProblemAlert } from '@porto/shared/ui/patterns';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { TASK_PRIORITIES, actionNeedsDialog, proposalOf, rangeInside } from '../lib';

/**
 * The action buttons of an alert (m12-plan §8.3): only the actions the server allows to the principal. Confirm
 * arrival and revoke act at once; release (range inside the proposal and the consent checkbox), task (priority)
 * and dismiss (note) open a dialog.
 */
export function AlertActions({
  marinaId,
  alert,
  size = 'sm',
}: {
  marinaId: string;
  alert: SensorAlert;
  size?: 'sm' | 'default';
}) {
  const { t } = useT();
  const act = useActOnSensorAlert(marinaId);
  const [dialog, setDialog] = useState<SensorAlertAction | null>(null);
  const run = (action: SensorAlertAction) => {
    if (actionNeedsDialog(action)) {
      setDialog(action);
      return;
    }
    act.mutate({ id: alert.id, version: alert.version, body: { action } });
  };
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        {alert.allowed_actions.map((a) => (
          <Button
            key={a}
            size={size}
            variant={a === 'dismiss' ? 'outline' : 'default'}
            disabled={act.isPending}
            data-testid={`alert-action-${a}`}
            onClick={() => run(a)}
          >
            {t(`sensors:action.${a}`)}
          </Button>
        ))}
      </div>
      {dialog === null ? <ProblemAlert error={act.error} /> : null}
      {dialog ? (
        <ActionDialog
          marinaId={marinaId}
          alert={alert}
          action={dialog}
          onClose={() => setDialog(null)}
        />
      ) : null}
    </div>
  );
}

function ActionDialog({
  marinaId,
  alert,
  action,
  onClose,
}: {
  marinaId: string;
  alert: SensorAlert;
  action: SensorAlertAction;
  onClose: () => void;
}) {
  const { t } = useT();
  const act = useActOnSensorAlert(marinaId);
  const proposal = proposalOf(alert);
  const [from, setFrom] = useState(proposal?.from ?? '');
  const [to, setTo] = useState(proposal?.to ?? '');
  const [consent, setConsent] = useState(false);
  const [priority, setPriority] = useState<(typeof TASK_PRIORITIES)[number]>('normal');
  const [note, setNote] = useState('');
  const rangeOk = !proposal || rangeInside(proposal, from, to);
  const canSubmit =
    action === 'confirm_release' ? consent && rangeOk && !!from && !!to : note.length <= 500;
  const submit = () => {
    const body: SensorAlertActionInput = { action };
    if (action === 'confirm_release') Object.assign(body, { from, to, consent_confirmed: consent });
    if (action === 'create_task') body.priority = priority;
    if (action === 'dismiss' && note.trim() !== '') body.note = note.trim();
    act.mutate({ id: alert.id, version: alert.version, body }, { onSuccess: onClose });
  };
  return (
    <Dialog open onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent data-testid={`alert-dialog-${action}`}>
        <DialogHeader>
          <DialogTitle>{t(`sensors:action.${action}`)}</DialogTitle>
          <DialogDescription>
            {t(`sensors:alertKind.${alert.kind}`)} · {alert.resource_code}
          </DialogDescription>
        </DialogHeader>
        {action === 'confirm_release' ? (
          <div className="flex flex-col gap-3">
            <div className="flex flex-wrap gap-3">
              <Field className="w-auto">
                <FieldLabel htmlFor="release-from">{t('sensors:dialog.from')}</FieldLabel>
                <Input
                  id="release-from"
                  type="date"
                  value={from}
                  min={proposal?.from}
                  max={proposal?.to}
                  onChange={(e) => setFrom(e.target.value)}
                />
              </Field>
              <Field className="w-auto">
                <FieldLabel htmlFor="release-to">{t('sensors:dialog.to')}</FieldLabel>
                <Input
                  id="release-to"
                  type="date"
                  value={to}
                  min={proposal?.from}
                  max={proposal?.to}
                  onChange={(e) => setTo(e.target.value)}
                />
              </Field>
            </div>
            {!rangeOk ? (
              <p className="text-destructive text-sm" role="alert">
                {t('sensors:dialog.outsideProposal')}
              </p>
            ) : null}
            <Field orientation="horizontal">
              <Checkbox
                id="release-consent"
                checked={consent}
                onCheckedChange={(v) => setConsent(v === true)}
                data-testid="release-consent"
              />
              <FieldLabel htmlFor="release-consent">{t('sensors:dialog.consent')}</FieldLabel>
            </Field>
          </div>
        ) : null}
        {action === 'create_task' ? (
          <Field>
            <FieldLabel htmlFor="task-priority">{t('sensors:dialog.priority')}</FieldLabel>
            <Select
              value={priority}
              onValueChange={(v) => setPriority(v as (typeof TASK_PRIORITIES)[number])}
            >
              <SelectTrigger id="task-priority">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {TASK_PRIORITIES.map((p) => (
                  <SelectItem key={p} value={p}>
                    {t(`sensors:priority.${p}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ) : null}
        {action === 'dismiss' ? (
          <Field>
            <FieldLabel htmlFor="dismiss-note">{t('sensors:dialog.note')}</FieldLabel>
            <Textarea
              id="dismiss-note"
              value={note}
              maxLength={500}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
        ) : null}
        <ProblemAlert error={act.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={!canSubmit || act.isPending}
            onClick={submit}
            data-testid="alert-dialog-submit"
          >
            {t(`sensors:action.${action}`)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
