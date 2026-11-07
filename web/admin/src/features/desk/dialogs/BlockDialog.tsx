import { useState } from 'react';

import { useCreateBlock, type PlacementInput } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Dialog, DialogContent } from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker, LengthInput } from '@porto/shared/ui/patterns';

import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { SheetFooter, SheetHeader } from '../components/SheetChrome';
import { isIsoDate } from '../lib/dates';

export type BlockDialogProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  target: PlacementInput;
  defaultFrom: string;
};

/** Manual block of a berth, of a footprint in a dynamic area or of a land resource, with a mandatory reason. */
export function BlockDialog(props: BlockDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent data-testid="desk-block-dialog">
        {props.open ? <BlockForm {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

export function BlockForm({ marinaId, onOpenChange, target, defaultFrom }: BlockDialogProps) {
  const { t, i18n } = useT();
  const create = useCreateBlock(marinaId);
  const [from, setFrom] = useState<string | null>(defaultFrom);
  const [to, setTo] = useState<string | null>(defaultFrom);
  const [reason, setReason] = useState('');
  const [loa, setLoa] = useState<number | null>(null);
  const [beam, setBeam] = useState<number | null>(null);
  const area = target.resource_type === 'dynamic_area';
  const valid =
    isIsoDate(from) &&
    isIsoDate(to) &&
    from <= to &&
    reason.trim() !== '' &&
    (!area || (loa !== null && loa > 0 && beam !== null && beam > 0));
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!valid) return;
        create.mutate(
          {
            body: {
              target,
              from,
              to,
              reason: reason.trim(),
              ...(area ? { loa_cm: loa!, beam_cm: beam! } : {}),
            },
          },
          { onSuccess: () => onOpenChange(false) },
        );
      }}
    >
      <SheetHeader title={t('desk:block.title')} description={t('desk:block.description')} />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="block-from">{t('desk:block.from')}</FieldLabel>
          <DatePicker id="block-from" value={from} onChange={setFrom} />
        </Field>
        <Field>
          <FieldLabel htmlFor="block-to">{t('desk:block.to')}</FieldLabel>
          <DatePicker id="block-to" value={to} onChange={setTo} />
        </Field>
      </div>
      {area ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="block-loa">{t('desk:block.length')}</FieldLabel>
            <LengthInput id="block-loa" locale={i18n.language} value={loa} onChange={setLoa} />
          </Field>
          <Field>
            <FieldLabel htmlFor="block-beam">{t('desk:block.width')}</FieldLabel>
            <LengthInput id="block-beam" locale={i18n.language} value={beam} onChange={setBeam} />
          </Field>
        </div>
      ) : null}
      <Field data-invalid={reason.trim() === '' || undefined}>
        <FieldLabel htmlFor="block-reason">{t('desk:block.reason')}</FieldLabel>
        <Input
          id="block-reason"
          value={reason}
          maxLength={200}
          onChange={(e) => setReason(e.target.value)}
        />
        {reason.trim() === '' ? <FieldError>{t('common:validation.required')}</FieldError> : null}
      </Field>
      <DeskProblemAlert error={create.error} />
      <SheetFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={!valid || create.isPending}>
          {t('desk:block.submit')}
        </Button>
      </SheetFooter>
    </form>
  );
}
