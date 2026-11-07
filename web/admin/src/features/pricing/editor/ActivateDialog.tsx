import { useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  overlapConflictsOf,
  priceListIssuesOf,
  useActivatePriceList,
  type PriceListActivateResult,
  type PriceListDocument,
  type PriceListIssue,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { DatePicker } from '@porto/shared/ui/patterns';

import { toastProblem } from '../../../lib/toast';

type Conflict = { id: string; code: string; valid_from: string; valid_to: string };

/**
 * Activation at a date (m2-plan §4.4, INV-24): predecessors serving the same contract types are truncated
 * (shown after success); an overlap that cannot be truncated is listed; validation errors go to the tab.
 */
export function ActivateDialog({
  marinaId,
  doc,
  open,
  onClose,
  onInvalid,
}: {
  marinaId: string;
  doc: PriceListDocument;
  open: boolean;
  onClose: () => void;
  onInvalid: (issues: PriceListIssue[]) => void;
}) {
  const { t } = useT();
  return (
    <Dialog open={open} onOpenChange={(o) => (o ? null : onClose())}>
      <DialogContent>
        {open ? (
          <ActivateBody marinaId={marinaId} doc={doc} onClose={onClose} onInvalid={onInvalid} />
        ) : (
          <DialogTitle className="sr-only">{t('pricing:editor.activate.title')}</DialogTitle>
        )}
      </DialogContent>
    </Dialog>
  );
}

function ActivateBody({
  marinaId,
  doc,
  onClose,
  onInvalid,
}: {
  marinaId: string;
  doc: PriceListDocument;
  onClose: () => void;
  onInvalid: (issues: PriceListIssue[]) => void;
}) {
  const { t } = useT();
  const activate = useActivatePriceList(marinaId);
  const [date, setDate] = useState<string | null>(doc.valid_from);
  const [conflicts, setConflicts] = useState<Conflict[] | null>(null);
  const [done, setDone] = useState<PriceListActivateResult | null>(null);
  const outside = date !== null && (date < doc.valid_from || date > doc.valid_to);

  const submit = async () => {
    setConflicts(null);
    try {
      const res = await activate.mutateAsync({
        id: doc.id,
        version: doc.version,
        ...(date && date !== doc.valid_from ? { effectiveFrom: date } : {}),
      });
      toast.success(t('pricing:editor.activate.done'));
      if (res.truncated.length === 0) onClose();
      else setDone(res);
    } catch (err) {
      if (isProblem(err, 'price_list_overlap')) {
        setConflicts(overlapConflictsOf(err));
      } else if (isProblem(err, 'price_list_invalid')) {
        onInvalid(priceListIssuesOf(err));
        toast.error(t('errors:price_list_invalid'));
        onClose();
      } else {
        toastProblem(t, err);
      }
    }
  };

  if (done) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>{t('pricing:editor.activate.done')}</DialogTitle>
          <DialogDescription>{t('pricing:editor.activate.truncatedHint')}</DialogDescription>
        </DialogHeader>
        <ul className="flex flex-col gap-1 text-sm" data-testid="truncated">
          {done.truncated.map((r) => (
            <li key={r.id}>
              {t('pricing:editor.activate.truncated', {
                code: r.code,
                to: formatIsoDate(r.valid_to),
              })}
            </li>
          ))}
        </ul>
        <DialogFooter>
          <Button onClick={onClose}>{t('common:actions.close')}</Button>
        </DialogFooter>
      </>
    );
  }

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <DialogHeader>
        <DialogTitle>{t('pricing:editor.activate.title')}</DialogTitle>
        <DialogDescription>{t('pricing:editor.activate.description')}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-4 py-4">
        <Field data-invalid={outside || undefined}>
          <FieldLabel htmlFor="activate-effective">
            {t('pricing:editor.activate.effectiveFrom')}
          </FieldLabel>
          <DatePicker
            id="activate-effective"
            value={date}
            onChange={setDate}
            aria-invalid={outside}
          />
          <FieldDescription>
            {t('pricing:editor.activate.validity', {
              from: formatIsoDate(doc.valid_from),
              to: formatIsoDate(doc.valid_to),
            })}
          </FieldDescription>
        </Field>
        {conflicts ? (
          <Alert variant="destructive">
            <AlertTitle>{t('errors:price_list_overlap')}</AlertTitle>
            <AlertDescription>
              <ul className="flex flex-col gap-1">
                {conflicts.map((c) => (
                  <li key={c.id}>
                    {t('pricing:editor.activate.conflict', {
                      code: c.code,
                      from: formatIsoDate(c.valid_from),
                      to: formatIsoDate(c.valid_to),
                    })}
                  </li>
                ))}
              </ul>
            </AlertDescription>
          </Alert>
        ) : null}
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={activate.isPending || outside || date === null}>
          {t('pricing:editor.activate.confirm')}
        </Button>
      </DialogFooter>
    </form>
  );
}
