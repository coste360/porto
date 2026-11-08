import { useState } from 'react';
import { toast } from 'sonner';

import { PRESENCE_PHOTO_MAX_BYTES, PRESENCE_PHOTO_TYPES } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { enqueue } from '@porto/shared/offline';
import { Button } from '@porto/shared/ui/button';
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { presenceOp, presencePhotoOp, type PresenceEvent } from '../lib/ops';
import { reencodePhoto } from '../lib/photo';

export type PresenceTarget = {
  vessel: { id: string; name: string };
  allocationId: string;
  resourceId: string;
  event: PresenceEvent;
};

/**
 * Arrived / Departed of the phone (m6b-plan §6.2): an optional note and a camera photo, queued (queue first, D2)
 * with the allocation and the berth the worker saw. The queue sends at once when online.
 */
export function PresenceSheet({
  target,
  onOpenChange,
}: {
  target: PresenceTarget | null;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Drawer open={target !== null} onOpenChange={onOpenChange}>
      {/* Same test id as the M3 presence dialog: the M3 end-to-end specs drive this sheet. */}
      <DrawerContent className="p-4" data-testid="desk-presence-dialog">
        {target ? <PresenceForm target={target} onDone={() => onOpenChange(false)} /> : null}
      </DrawerContent>
    </Drawer>
  );
}

function PresenceForm({ target, onDone }: { target: PresenceTarget; onDone: () => void }) {
  const { t } = useT();
  const { marinaId, me } = useCurrentMarina();
  const [notes, setNotes] = useState('');
  const [photo, setPhoto] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [tooBig, setTooBig] = useState(false);
  // The type is checked here; the size after the re-encoding (a phone picture shrinks well below 8 MiB).
  const fileError =
    photo && !(PRESENCE_PHOTO_TYPES as readonly string[]).includes(photo.type)
      ? 'desk:presence.photoType'
      : tooBig
        ? 'desk:presence.photoSize'
        : null;
  const arrived = target.event === 'arrived';

  const submit = async () => {
    if (fileError || !me) return;
    setBusy(true);
    setFailed(false);
    const scope = { userId: me.user.id, marinaId };
    try {
      const blob = photo ? await reencodePhoto(photo) : null;
      if (blob && blob.size > PRESENCE_PHOTO_MAX_BYTES) {
        setTooBig(true);
        return;
      }
      const parent = await enqueue(
        presenceOp(scope, {
          vesselId: target.vessel.id,
          vesselName: target.vessel.name,
          event: target.event,
          allocationId: target.allocationId,
          resourceId: target.resourceId,
          notes,
          at: new Date().toISOString(),
        }),
      );
      if (blob) {
        await enqueue(
          presencePhotoOp(scope, {
            parentOpId: parent.id,
            vesselId: target.vessel.id,
            vesselName: target.vessel.name,
            blob,
          }),
        );
      }
      toast.success(t('phone:dock.presenceSheet.queuedToast'));
      onDone();
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <DrawerHeader className="px-0">
        <DrawerTitle>
          {arrived
            ? t('phone:dock.presenceSheet.arrivedTitle', { name: target.vessel.name })
            : t('phone:dock.presenceSheet.departedTitle', { name: target.vessel.name })}
        </DrawerTitle>
        <DrawerDescription>{t('desk:presence.description')}</DrawerDescription>
      </DrawerHeader>
      <Field>
        <FieldLabel htmlFor="phone-presence-notes">{t('desk:fields.notes')}</FieldLabel>
        <Textarea
          id="phone-presence-notes"
          value={notes}
          maxLength={1000}
          onChange={(e) => setNotes(e.target.value)}
        />
        <FieldDescription>{t('phone:dock.presenceSheet.notesHint')}</FieldDescription>
      </Field>
      <Field data-invalid={fileError ? true : undefined}>
        <FieldLabel htmlFor="phone-presence-photo">{t('phone:dock.photo')}</FieldLabel>
        <Input
          id="phone-presence-photo"
          type="file"
          accept={PRESENCE_PHOTO_TYPES.join(',')}
          capture="environment"
          className="min-h-11"
          onChange={(e) => {
            setTooBig(false);
            setPhoto(e.target.files?.[0] ?? null);
          }}
        />
        <FieldDescription>{t('phone:dock.presenceSheet.photoHint')}</FieldDescription>
        {fileError ? <FieldError>{t(fileError)}</FieldError> : null}
      </Field>
      {failed ? (
        <p className="text-destructive text-sm" role="alert">
          {t('phone:enqueueFailed')}
        </p>
      ) : null}
      <DrawerFooter className="px-0">
        <Button type="submit" className="min-h-11" disabled={busy || fileError !== null}>
          {arrived
            ? t('phone:dock.presenceSheet.submitArrived')
            : t('phone:dock.presenceSheet.submitDeparted')}
        </Button>
        <Button type="button" variant="outline" className="min-h-11" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
      </DrawerFooter>
    </form>
  );
}
