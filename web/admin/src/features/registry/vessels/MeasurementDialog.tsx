import { zodResolver } from '@hookform/resolvers/zod';
import { RulerIcon } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { useRecordVesselMeasurement, type Vessel } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { FieldGroup } from '@porto/shared/ui/field';
import { ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { LengthField } from '../components/fields';

const length = z.number().int().positive('vessels:validation.positiveLength').nullable();
const schema = z.object({
  measured_loa_cm: length.refine((v): boolean => v !== null, 'vessels:validation.positiveLength'),
  measured_beam_cm: length.refine((v): boolean => v !== null, 'vessels:validation.positiveLength'),
  measured_draft_cm: length,
  measured_air_draft_cm: length,
});
type Values = z.infer<typeof schema>;

/** Dimensions measured on the dock (customers.write or presence.write, D12). */
export function MeasurementDialog({ vessel }: { vessel: Vessel }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [open, setOpen] = useState(false);
  const record = useRecordVesselMeasurement(marinaId);
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      measured_loa_cm: vessel.measured_loa_cm ?? vessel.loa_cm,
      measured_beam_cm: vessel.measured_beam_cm ?? vessel.beam_cm,
      measured_draft_cm: vessel.measured_draft_cm ?? vessel.draft_cm,
      measured_air_draft_cm: vessel.measured_air_draft_cm ?? vessel.air_draft_cm,
    },
  });
  const submit = form.handleSubmit((v) =>
    record.mutate(
      {
        vesselId: vessel.id,
        measurement: {
          loa_cm: v.measured_loa_cm!,
          beam_cm: v.measured_beam_cm!,
          ...(v.measured_draft_cm !== null ? { draft_cm: v.measured_draft_cm } : {}),
          ...(v.measured_air_draft_cm !== null ? { air_draft_cm: v.measured_air_draft_cm } : {}),
        },
      },
      {
        onSuccess: () => {
          toast.success(t('vessels:measurement.saved'));
          setOpen(false);
        },
      },
    ),
  );
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) record.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline">
          <RulerIcon />
          {t('vessels:measurement.action')}
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('vessels:measurement.title')}</DialogTitle>
          <DialogDescription>{t('vessels:measurement.description')}</DialogDescription>
        </DialogHeader>
        <form noValidate className="flex flex-col gap-4" onSubmit={submit}>
          {record.isError ? <ProblemAlert error={record.error} /> : null}
          <FieldGroup className="grid gap-4 sm:grid-cols-2">
            <LengthField
              control={form.control}
              name="measured_loa_cm"
              label={t('vessels:fields.loa')}
            />
            <LengthField
              control={form.control}
              name="measured_beam_cm"
              label={t('vessels:fields.beam')}
            />
            <LengthField
              control={form.control}
              name="measured_draft_cm"
              label={t('vessels:fields.draft')}
            />
            <LengthField
              control={form.control}
              name="measured_air_draft_cm"
              label={t('vessels:fields.airDraft')}
            />
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              {t('common:actions.cancel')}
            </Button>
            <Button type="submit" disabled={record.isPending}>
              {t('common:actions.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
