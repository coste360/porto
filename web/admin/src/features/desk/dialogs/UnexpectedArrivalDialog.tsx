import { Link } from '@tanstack/react-router';
import { useState } from 'react';

import { useRecordUnexpectedArrival, type Contract, type PlacementInput } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Dialog, DialogContent } from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { useMarinaToday } from '../../registry/lib/today';
import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { SheetFooter, SheetHeader } from '../components/SheetChrome';
import { ResourceField } from '../components/ResourceField';
import { VesselField } from '../components/VesselField';
import { emptyNewVessel, newVesselInput, vesselDims, type VesselValue } from '../lib/vessel';
import { addDays, isIsoDate, lastNightOf } from '../lib/dates';

export type UnexpectedArrivalDialogProps = {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  target?: PlacementInput;
};

/** Arrival without booking (9.5, D29): vessel (new with estimated dimensions, or existing) and place → held transit + arrival. */
export function UnexpectedArrivalDialog(props: UnexpectedArrivalDialogProps) {
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className="max-h-screen overflow-y-auto sm:max-w-xl"
        data-testid="desk-unexpected-dialog"
      >
        {props.open ? <UnexpectedArrivalForm {...props} /> : null}
      </DialogContent>
    </Dialog>
  );
}

export function UnexpectedArrivalForm({
  marinaId,
  onOpenChange,
  target: target0,
}: UnexpectedArrivalDialogProps) {
  const { t } = useT();
  const { can } = useCurrentMarina();
  const today = useMarinaToday();
  const record = useRecordUnexpectedArrival(marinaId);
  const [vessel, setVessel] = useState<VesselValue>(emptyNewVessel());
  const [target, setTarget] = useState<PlacementInput | null>(target0 ?? null);
  const [departure, setDeparture] = useState<string | null>(addDays(today, 1));
  const [notes, setNotes] = useState('');
  const [created, setCreated] = useState<Contract | null>(null);
  const vesselOk =
    vessel.kind === 'existing' ? vessel.id !== null : newVesselInput(vessel) !== null;
  const valid = vesselOk && target !== null && isIsoDate(departure) && departure > today;

  const submit = (override?: string) => {
    if (!valid || !target) return;
    const nv = newVesselInput(vessel);
    record.mutate(
      {
        target,
        departure: departure!,
        ...(vessel.kind === 'existing' && vessel.id ? { vessel_id: vessel.id } : {}),
        ...(nv ? { new_vessel: nv } : {}),
        ...(notes.trim() ? { notes: notes.trim() } : {}),
        ...(override ? { fit_override: { reason: override } } : {}),
      },
      { onSuccess: (r) => setCreated(r.contract ?? null) },
    );
  };

  if (record.isSuccess) {
    return (
      <>
        <SheetHeader
          title={t('desk:unexpected.done')}
          description={t('desk:unexpected.doneHint')}
        />
        <SheetFooter>
          {created && can('contracts.read') ? (
            <Button variant="outline" asChild>
              <Link
                to="/m/$marinaId/transits/$contractId"
                params={{ marinaId, contractId: created.id }}
              >
                {t('desk:transit.open')}
              </Link>
            </Button>
          ) : null}
          <Button onClick={() => onOpenChange(false)}>{t('common:actions.close')}</Button>
        </SheetFooter>
      </>
    );
  }

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <SheetHeader
        title={t('desk:unexpected.title')}
        description={t('desk:unexpected.description')}
      />
      <VesselField
        marinaId={marinaId}
        idPrefix="unexpected"
        value={vessel}
        onChange={setVessel}
        allowNew
      />
      <ResourceField
        marinaId={marinaId}
        idPrefix="unexpected"
        value={target}
        onChange={setTarget}
        vessel={vesselDims(vessel)}
        date={today}
        from={today}
        to={departure && departure > today ? lastNightOf(departure) : today}
      />
      <Field>
        <FieldLabel htmlFor="unexpected-departure">{t('desk:fields.departure')}</FieldLabel>
        <DatePicker id="unexpected-departure" value={departure} onChange={setDeparture} />
      </Field>
      <Field>
        <FieldLabel htmlFor="unexpected-notes">{t('desk:fields.notes')}</FieldLabel>
        <Textarea
          id="unexpected-notes"
          value={notes}
          maxLength={1000}
          onChange={(e) => setNotes(e.target.value)}
        />
      </Field>
      <DeskProblemAlert
        error={record.error}
        {...(can('occupancy.override_fit') ? { onOverride: (r: string) => submit(r) } : {})}
      />
      <SheetFooter>
        <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={!valid || record.isPending} className="min-h-11">
          {t('desk:unexpected.submit')}
        </Button>
      </SheetFooter>
    </form>
  );
}
