import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { MapIcon, PencilIcon } from 'lucide-react';

import {
  useStorageContract,
  useUpdateStorageContract,
  type Contract,
  type StorageSupport,
  type StorageSupportOwner,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@porto/shared/ui/dialog';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker, ProblemAlert } from '@porto/shared/ui/patterns';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../../auth/hooks';
import { storagePatchBody } from '../lib/contract';

const SUPPORTS = ['cradle', 'trailer', 'rack', 'none'] as const;
const OWNERS = ['own', 'marina'] as const;

/**
 * Storage section of the contract detail page (m9-plan §12.1): support, planned haul-out and launch, exit and
 * arrival dates, handling notes (edited in place with If-Match) and the link to the planner of the area.
 */
export function StorageContractSection({ contract }: { contract: Contract }) {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const enabled = contract.kind === 'storage' && can('storage.read');
  const sc = useStorageContract(marinaId, enabled ? contract.id : undefined);
  if (!enabled || !sc.data) return null;
  const d = sc.data.details;
  const pl = sc.data.placement;
  const rows: [string, string][] = [
    ['support', t(`storage:support.${d.support}`)],
    ['supportOwner', t(`storage:supportOwner.${d.support_owner}`)],
    ['haulOutOn', d.haul_out_on ? formatIsoDate(d.haul_out_on) : '—'],
    ['launchOn', d.launch_on ? formatIsoDate(d.launch_on) : '—'],
    ['arrivalOn', formatIsoDate(d.arrival_on)],
    ['exitOn', formatIsoDate(d.exit_on)],
  ];
  return (
    <Card className="gap-3 py-4" data-testid="storage-contract-section">
      <CardHeader className="px-4">
        <CardTitle className="text-base">{t('storage:contract.section')}</CardTitle>
        <CardAction className="flex gap-2">
          {pl && pl.resource_type === 'dynamic_area' ? (
            <Button variant="outline" size="sm" asChild>
              <Link
                to="/m/$marinaId/storage/$areaId"
                params={{ marinaId, areaId: pl.resource_id }}
                search={{ date: pl.first_night }}
              >
                <MapIcon />
                {t('storage:contract.openPlanner')}
              </Link>
            </Button>
          ) : null}
          {can('storage.manage') ? (
            <EditDetailsDialog contractId={contract.id} details={d} />
          ) : null}
        </CardAction>
      </CardHeader>
      <CardContent className="px-4">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{t(`storage:contract.${k}`)}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
        {d.notes_handling ? (
          <p className="mt-2 text-sm whitespace-pre-line">{d.notes_handling}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function EditDetailsDialog({
  contractId,
  details,
}: {
  contractId: string;
  details: {
    support: StorageSupport;
    support_owner: StorageSupportOwner;
    haul_out_on?: string;
    launch_on?: string;
    notes_handling?: string;
    version: number;
  };
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const update = useUpdateStorageContract(marinaId, contractId);
  const [open, setOpen] = useState(false);
  const [support, setSupport] = useState<StorageSupport>(details.support);
  const [owner, setOwner] = useState<StorageSupportOwner>(details.support_owner);
  const [haul, setHaul] = useState<string | null>(details.haul_out_on ?? null);
  const [launch, setLaunch] = useState<string | null>(details.launch_on ?? null);
  const [notes, setNotes] = useState(details.notes_handling ?? '');
  const invalid = !!haul && !!launch && launch < haul;
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <PencilIcon />
          {t('storage:contract.edit')}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('storage:contract.edit')}</DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (invalid) return;
            update.mutate(
              {
                body: storagePatchBody({
                  support,
                  supportOwner: owner,
                  haulOutOn: haul,
                  launchOn: launch,
                  notesHandling: notes,
                }),
                version: details.version,
              },
              { onSuccess: () => setOpen(false) },
            );
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="sd-support">{t('storage:contract.support')}</FieldLabel>
              <Select value={support} onValueChange={(v) => setSupport(v as StorageSupport)}>
                <SelectTrigger id="sd-support">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SUPPORTS.map((x) => (
                    <SelectItem key={x} value={x}>
                      {t(`storage:support.${x}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="sd-owner">{t('storage:contract.supportOwner')}</FieldLabel>
              <Select value={owner} onValueChange={(v) => setOwner(v as StorageSupportOwner)}>
                <SelectTrigger id="sd-owner">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {OWNERS.map((x) => (
                    <SelectItem key={x} value={x}>
                      {t(`storage:supportOwner.${x}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="sd-haul">{t('storage:contract.haulOutOn')}</FieldLabel>
              <DatePicker id="sd-haul" value={haul} onChange={setHaul} />
            </Field>
            <Field>
              <FieldLabel htmlFor="sd-launch">{t('storage:contract.launchOn')}</FieldLabel>
              <DatePicker
                id="sd-launch"
                value={launch}
                onChange={setLaunch}
                aria-invalid={invalid}
              />
            </Field>
          </div>
          <Field>
            <FieldLabel htmlFor="sd-notes">{t('storage:contract.notesHandling')}</FieldLabel>
            <Textarea
              id="sd-notes"
              maxLength={500}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
            />
          </Field>
          {invalid ? (
            <p className="text-destructive text-sm">{t('storage:contract.errors.launch')}</p>
          ) : null}
          <ProblemAlert error={update.error} />
          <DialogFooter>
            <Button type="submit" disabled={invalid || update.isPending}>
              {t('common:actions.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
