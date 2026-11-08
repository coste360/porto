import { useState, type FormEvent } from 'react';

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
import { Field, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns';

/**
 * Asks for the app PIN of the remote mode before a local administration call (m6b-plan §7.5). The PIN lives only
 * in this dialog's state and is forgotten when it closes; the caller sends it in the Porto-App-Pin header.
 */
export function PinDialog({
  open,
  onOpenChange,
  onConfirm,
  pending,
  error,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (pin: string) => void;
  pending?: boolean;
  error?: unknown;
}) {
  const { t } = useT();
  const [pin, setPin] = useState('');
  const valid = /^[0-9]{6,12}$/.test(pin);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (valid) onConfirm(pin);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setPin('');
        onOpenChange(o);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('desktop:pin.title')}</DialogTitle>
          <DialogDescription>{t('desktop:pin.description')}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} noValidate>
          <FieldGroup>
            {error ? <ProblemAlert error={error} /> : null}
            <Field>
              <FieldLabel htmlFor="app-pin">{t('desktop:appPin')}</FieldLabel>
              <Input
                id="app-pin"
                type="password"
                inputMode="numeric"
                autoComplete="off"
                value={pin}
                onChange={(e) => setPin(e.target.value.replace(/\D/g, '').slice(0, 12))}
              />
            </Field>
            <DialogFooter>
              <Button type="submit" disabled={!valid || pending}>
                {t('desktop:pin.confirm')}
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  );
}
