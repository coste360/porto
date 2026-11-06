import { CheckIcon, CopyIcon, TriangleAlertIcon } from 'lucide-react';
import { useState } from 'react';

import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';

import { useT } from '../../i18n/useT';

export type SecretRevealProps = {
  /** The secret, shown once: the server never returns it again. */
  secret: string;
  label: string;
  onDone: () => void;
};

/** Shows a just-created secret (API key, webhook secret) once, with copy and a warning. */
export function SecretReveal({ secret, label, onDone }: SecretRevealProps) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  const id = 'secret-reveal';

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <TriangleAlertIcon />
        <AlertTitle>{t('common:secret.title')}</AlertTitle>
        <AlertDescription>{t('common:secret.warning')}</AlertDescription>
      </Alert>
      <div className="flex flex-col gap-2">
        <Label htmlFor={id}>{label}</Label>
        <div className="flex gap-2">
          <Input id={id} readOnly value={secret} className="font-mono" />
          <Button type="button" variant="outline" onClick={() => void copy()}>
            {copied ? <CheckIcon /> : <CopyIcon />}
            {copied ? t('common:actions.copied') : t('common:actions.copy')}
          </Button>
        </div>
      </div>
      <div className="flex justify-end">
        <Button type="button" onClick={onDone}>
          {t('common:secret.done')}
        </Button>
      </div>
    </div>
  );
}
