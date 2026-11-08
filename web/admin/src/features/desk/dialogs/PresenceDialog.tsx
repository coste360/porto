import { useState } from 'react';
import { toast } from 'sonner';

import { PRESENCE_PHOTO_TYPES, useRecordPresence, useUploadPresencePhoto } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { isQueuedOffline } from '@porto/shared/offline';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { DatePicker } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { photoError } from '../lib/photo';

export type PresenceDialogProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  vesselId: string;
  allocationId?: string;
  event: 'arrived' | 'departed';
  /** Shown in the title (the vessel name), when the caller has it. */
  vesselName?: string;
};

/** Register an arrival or a departure now, with an optional note and photo (9.5). */
export function PresenceDialog(props: PresenceDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent data-testid="desk-presence-dialog">
        {props.open ? <PresenceForm {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function PresenceForm({
  marinaId,
  onOpenChange,
  vesselId,
  allocationId,
  event,
  vesselName,
}: PresenceDialogProps) {
  const { t } = useT();
  const record = useRecordPresence(marinaId);
  const upload = useUploadPresencePhoto(marinaId);
  const [notes, setNotes] = useState('');
  const [photo, setPhoto] = useState<File | null>(null);
  const [expectedReturn, setExpectedReturn] = useState<string | null>(null);
  const fileError = photoError(photo);
  const busy = record.isPending || upload.isPending;
  // A queued operation (offline, or no answer yet) is not an error: the queue sends it later (m6b-plan §4.3).
  const closeIfQueued = (err: unknown) => {
    if (!isQueuedOffline(err)) return;
    toast(t('offline:queuedBadge'));
    onOpenChange(false);
  };
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (fileError) return;
        record.mutate(
          {
            vessel_id: vesselId,
            event,
            ...(allocationId ? { allocation_id: allocationId } : {}),
            ...(notes.trim() ? { notes: notes.trim() } : {}),
            ...(event === 'departed' && expectedReturn
              ? { expected_return_on: expectedReturn }
              : {}),
            ...(vesselName ? { vesselName } : {}),
          },
          {
            onSuccess: (r) => {
              if (photo)
                upload.mutate(
                  { presenceId: r.presence.id, file: photo, vesselId, vesselName },
                  { onSuccess: () => onOpenChange(false), onError: closeIfQueued },
                );
              else onOpenChange(false);
            },
            onError: closeIfQueued,
          },
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>
          {event === 'arrived'
            ? t('desk:presence.arrivedTitle', { name: vesselName ?? '' })
            : t('desk:presence.departedTitle', { name: vesselName ?? '' })}
        </DialogTitle>
        <DialogDescription>{t('desk:presence.description')}</DialogDescription>
      </DialogHeader>
      <Field>
        <FieldLabel htmlFor="presence-notes">{t('desk:fields.notes')}</FieldLabel>
        <Textarea
          id="presence-notes"
          value={notes}
          maxLength={1000}
          onChange={(e) => setNotes(e.target.value)}
        />
      </Field>
      {event === 'departed' ? (
        <Field>
          <FieldLabel htmlFor="presence-return">{t('desk:presence.expectedReturn')}</FieldLabel>
          <DatePicker id="presence-return" value={expectedReturn} onChange={setExpectedReturn} />
        </Field>
      ) : null}
      <Field data-invalid={fileError ? true : undefined}>
        <FieldLabel htmlFor="presence-photo">{t('desk:presence.photo')}</FieldLabel>
        <Input
          id="presence-photo"
          type="file"
          accept={PRESENCE_PHOTO_TYPES.join(',')}
          capture="environment"
          onChange={(e) => setPhoto(e.target.files?.[0] ?? null)}
        />
        <FieldDescription>{t('desk:presence.photoHint')}</FieldDescription>
        {fileError ? <FieldError>{t(fileError)}</FieldError> : null}
      </Field>
      <DeskProblemAlert error={queuedOrError(record.error) ?? queuedOrError(upload.error)} />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={busy || fileError !== null} className="min-h-11">
          {event === 'arrived'
            ? t('desk:presence.submitArrived')
            : t('desk:presence.submitDeparted')}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** Errors shown in the dialog: a queued operation is not one. */
function queuedOrError(err: Error | null): Error | null {
  return err && !isQueuedOffline(err) ? err : null;
}
