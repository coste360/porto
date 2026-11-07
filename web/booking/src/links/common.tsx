import { useEffect, useMemo } from 'react';

import { usePublicLinkDocument, type PublicDeposit } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

export type LinkRef = { slug: string; token: string };

export function Money({ cents }: { cents: number }) {
  const { i18n } = useT();
  return <span className="tabular-nums">{formatCents(cents, 'EUR', i18n.language)}</span>;
}

/** The link's PDF in an <iframe> of a blob URL (never a public file URL); revoked on unmount. */
export function PdfViewer({ linkRef, title }: { linkRef: LinkRef; title: string }) {
  const { t } = useT();
  const doc = usePublicLinkDocument(linkRef);
  const url = useMemo(() => (doc.data ? URL.createObjectURL(doc.data) : null), [doc.data]);
  useEffect(
    () => () => {
      if (url) URL.revokeObjectURL(url);
    },
    [url],
  );
  if (doc.isError) return <ProblemAlert error={doc.error} />;
  if (!url)
    return <Skeleton className="h-96 w-full" aria-label={t('customerLinks:document.loading')} />;
  return (
    <div className="flex flex-col gap-2">
      <iframe
        title={title}
        src={url}
        className="h-96 w-full rounded-md border"
        data-testid="link-pdf"
      />
      <a href={url} download className="text-sm underline underline-offset-4">
        {t('customerLinks:document.download')}
      </a>
    </div>
  );
}

/** Deposit instructions after acceptance or signature (D1): amount, due date, bank details, reference. */
export function DepositCard({ deposit }: { deposit: PublicDeposit }) {
  const { t } = useT();
  const b = deposit.bank_details;
  return (
    <Card data-testid="deposit">
      <CardHeader>
        <CardTitle>{t('customerLinks:deposit.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1 text-sm">
        <p>
          {t('customerLinks:deposit.amount')} <Money cents={deposit.amount_cents} />
        </p>
        <p>{t('customerLinks:deposit.dueOn', { date: formatIsoDate(deposit.due_on) })}</p>
        {b?.iban ? <p>{t('customerLinks:deposit.iban', { iban: b.iban })}</p> : null}
        {b?.holder ? <p>{t('customerLinks:deposit.holder', { holder: b.holder })}</p> : null}
        {b?.bank ? <p>{t('customerLinks:deposit.bank', { bank: b.bank })}</p> : null}
        {deposit.reference ? (
          <p>{t('customerLinks:deposit.reference', { reference: deposit.reference })}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

export function Done({ title, description }: { title: string; description?: string }) {
  return (
    <Alert role="status">
      <AlertTitle>{title}</AlertTitle>
      {description ? <AlertDescription>{description}</AlertDescription> : null}
    </Alert>
  );
}
