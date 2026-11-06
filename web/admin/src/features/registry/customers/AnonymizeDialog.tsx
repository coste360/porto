import { useState } from 'react';

import { useAnonymizeCustomer } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
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
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';

/** Irreversible GDPR anonymisation, confirmed by typing a word (D15). */
export function AnonymizeDialog({
  customerId,
  open,
  onOpenChange,
  onDone,
}: {
  customerId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const anonymize = useAnonymizeCustomer(marinaId);
  const [typed, setTyped] = useState('');
  const word = t('customers:anonymize.confirmWord');
  const close = (o: boolean) => {
    if (!o) {
      setTyped('');
      anonymize.reset();
    }
    onOpenChange(o);
  };
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('customers:anonymize.title')}</DialogTitle>
          <DialogDescription>{t('customers:anonymize.description')}</DialogDescription>
        </DialogHeader>
        {anonymize.isError ? <ProblemAlert error={anonymize.error} /> : null}
        <Field>
          <FieldLabel htmlFor="anonymize-confirm">
            {t('customers:anonymize.typeToConfirm', { word })}
          </FieldLabel>
          <Input
            id="anonymize-confirm"
            value={typed}
            autoComplete="off"
            onChange={(e) => setTyped(e.target.value)}
          />
        </Field>
        <DialogFooter>
          <Button variant="outline" onClick={() => close(false)}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            variant="destructive"
            disabled={typed.trim() !== word || anonymize.isPending}
            onClick={() =>
              anonymize.mutate(customerId, {
                onSuccess: () => {
                  close(false);
                  onDone();
                },
              })
            }
          >
            {t('customers:anonymize.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
