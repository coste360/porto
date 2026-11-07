import { useState } from 'react';

import { useCreateRelease, type Release, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Dialog, DialogContent } from '@porto/shared/ui/dialog';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { SheetFooter, SheetHeader } from '../components/SheetChrome';
import { isIsoDate } from '../lib/dates';
import { STAFF_RELEASE_SOURCES } from '../lib/enums';

type Source = Schemas['ReleaseSource'];

export type ReleaseDialogProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  allocationId: string;
  defaultFrom: string;
  /** Last night released by default (inclusive); the first night when absent. */
  defaultTo?: string;
  defaultSource?: Source;
  onReleased?: (r: Release) => void;
};

/** Release nights of a contract allocation for resale (10.6), with the staff-recorded consent source. */
export function ReleaseDialog(props: ReleaseDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent data-testid="desk-release-dialog">
        {props.open ? <ReleaseForm {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

export function ReleaseForm({
  marinaId,
  onOpenChange,
  allocationId,
  defaultFrom,
  defaultTo,
  defaultSource = 'office',
  onReleased,
}: ReleaseDialogProps) {
  const { t } = useT();
  const create = useCreateRelease(marinaId);
  const [from, setFrom] = useState<string | null>(defaultFrom);
  const [to, setTo] = useState<string | null>(defaultTo ?? defaultFrom);
  const [source, setSource] = useState<Source>(defaultSource);
  const [notes, setNotes] = useState('');
  const valid = isIsoDate(from) && isIsoDate(to) && from <= to;
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (!valid) return;
        create.mutate(
          {
            allocation_id: allocationId,
            from_night: from,
            to_night: to,
            source,
            ...(notes.trim() ? { notes: notes.trim() } : {}),
          },
          {
            onSuccess: (r) => {
              onReleased?.(r);
              onOpenChange(false);
            },
          },
        );
      }}
    >
      <SheetHeader title={t('desk:release.title')} description={t('desk:release.description')} />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="release-from">{t('desk:release.from')}</FieldLabel>
          <DatePicker id="release-from" value={from} onChange={setFrom} />
        </Field>
        <Field>
          <FieldLabel htmlFor="release-to">{t('desk:release.to')}</FieldLabel>
          <DatePicker id="release-to" value={to} onChange={setTo} />
        </Field>
      </div>
      {!valid ? <FieldError>{t('desk:release.invalidRange')}</FieldError> : null}
      <Field>
        <FieldLabel htmlFor="release-source">{t('desk:release.source')}</FieldLabel>
        <Select value={source} onValueChange={(v) => setSource(v as Source)}>
          <SelectTrigger id="release-source" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STAFF_RELEASE_SOURCES.map((s) => (
              <SelectItem key={s} value={s}>
                {t(`occupancyEnums:releaseSource.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
      <Field>
        <FieldLabel htmlFor="release-notes">{t('desk:fields.notes')}</FieldLabel>
        <Textarea
          id="release-notes"
          value={notes}
          maxLength={1000}
          onChange={(e) => setNotes(e.target.value)}
        />
      </Field>
      <DeskProblemAlert error={create.error} />
      <SheetFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={!valid || create.isPending}>
          {t('desk:release.submit')}
        </Button>
      </SheetFooter>
    </form>
  );
}
