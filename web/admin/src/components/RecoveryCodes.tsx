import { CopyIcon, DownloadIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';

/** Recovery codes, shown once, with copy and download. */
export function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const { t } = useT();
  const text = codes.join('\n');

  const download = () => {
    const url = URL.createObjectURL(new Blob([`${text}\n`], { type: 'text/plain' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = t('auth:totpSetup.recoveryFile');
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="flex flex-col gap-4">
      <Alert>
        <AlertTitle>{t('auth:totpSetup.recoveryTitle')}</AlertTitle>
        <AlertDescription>{t('auth:totpSetup.recoveryDescription')}</AlertDescription>
      </Alert>
      <ul className="bg-muted grid grid-cols-2 gap-2 rounded-md p-4 font-mono text-sm">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => void navigator.clipboard?.writeText(text)}
        >
          <CopyIcon />
          {t('common:actions.copy')}
        </Button>
        <Button type="button" variant="outline" onClick={download}>
          <DownloadIcon />
          {t('common:actions.download')}
        </Button>
        <Button type="button" className="ml-auto" onClick={onDone}>
          {t('auth:totpSetup.continue')}
        </Button>
      </div>
    </div>
  );
}
