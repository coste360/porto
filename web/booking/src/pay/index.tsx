import { CheckCircle2Icon, CopyIcon, CreditCardIcon, HourglassIcon } from 'lucide-react';
import { useState } from 'react';

import {
  ProblemError,
  usePayPage,
  usePayStatus,
  useStartPayCheckout,
  type PayBank,
  type PayPage as PayPageData,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatCents, formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { payView, type PayView } from './state';

type PayRef = { slug: string; token: string };

function Money({ cents }: { cents: number }) {
  const { i18n } = useT();
  return <span className="tabular-nums">{formatCents(cents, 'EUR', i18n.language)}</span>;
}

/**
 * Public payment page of a payment request (m5-plan §5.5, §9.1): amount, charges, bank transfer details with a
 * copy button and, only when an online provider is enabled, "Paga online". The return page polls the status
 * ("Pagamento in verifica" → "Pagamento ricevuto"). Phone first; the token is the only credential.
 */
export function PayPage({ slug, token, ret }: { slug: string; token: string; ret: boolean }) {
  const ref = { slug, token };
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-md flex-col gap-4 p-4">
      {ret ? <ReturnView payRef={ref} /> : <PayView payRef={ref} />}
    </main>
  );
}

function Loading() {
  const { t } = useT();
  return <Skeleton className="h-48 w-full" aria-label={t('payPublic:loading')} />;
}

function Invalid({ error }: { error: unknown }) {
  const { t } = useT();
  if (error instanceof ProblemError && error.status === 404)
    return (
      <EmptyState
        icon={<CreditCardIcon />}
        title={t('payPublic:invalid.title')}
        description={t('payPublic:invalid.description')}
      />
    );
  return <ProblemAlert error={error} />;
}

function PayView({ payRef }: { payRef: PayRef }) {
  const page = usePayPage(payRef);
  if (page.isPending) return <Loading />;
  if (page.isError) return <Invalid error={page.error} />;
  return <PayContent payRef={payRef} data={page.data} />;
}

function Header({ data }: { data: PayPageData }) {
  const { t } = useT();
  return (
    <header className="flex flex-col gap-1">
      <p className="text-muted-foreground text-sm">{data.marina.name}</p>
      <h1 className="text-2xl font-semibold">{t('payPublic:title')}</h1>
    </header>
  );
}

function PayContent({ payRef, data }: { payRef: PayRef; data: PayPageData }) {
  const { t } = useT();
  const view: PayView = payView(data.status, false);
  return (
    <>
      <Header data={data} />
      {view !== 'open' ? (
        <StatusAlert view={view} />
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle>{t('payPublic:amount')}</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <p className="text-3xl font-semibold" data-testid="pay-amount">
                <Money cents={data.amount_cents} />
              </p>
              {data.expires_at ? (
                <p className="text-muted-foreground text-xs">
                  {t('payPublic:expires', { date: formatIsoDate(data.expires_at.slice(0, 10)) })}
                </p>
              ) : null}
              <ul className="flex flex-col gap-2" aria-label={t('payPublic:charges')}>
                {data.charges.map((c) => (
                  <li
                    key={c.number}
                    className="flex items-start justify-between gap-3 text-sm"
                    data-testid="pay-charge"
                  >
                    <span className="flex flex-col">
                      <span className="font-medium">{c.description}</span>
                      <span className="text-muted-foreground text-xs">{c.number}</span>
                      <span className="text-muted-foreground text-xs">
                        {t('payPublic:dueOn', { date: formatIsoDate(c.due_on) })}
                      </span>
                    </span>
                    <Money cents={c.residual_cents} />
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
          {data.online ? <OnlineButtons payRef={payRef} methods={data.methods} /> : null}
          {data.bank ? <BankCard bank={data.bank} /> : null}
        </>
      )}
    </>
  );
}

function OnlineButtons({ payRef, methods }: { payRef: PayRef; methods: PayPageData['methods'] }) {
  const { t } = useT();
  const start = useStartPayCheckout(payRef);
  return (
    <div className="flex flex-col gap-2" data-testid="pay-online">
      {methods.map((m) => (
        <Button
          key={m}
          size="lg"
          disabled={start.isPending}
          onClick={() =>
            start.mutate(
              { method: m },
              { onSuccess: (r) => window.location.assign(r.redirect_url) },
            )
          }
        >
          <CreditCardIcon />
          {t(`payPublic:online.${m}`)}
        </Button>
      ))}
      <ProblemAlert error={start.error} />
    </div>
  );
}

function CopyLine({ label, value }: { label: string; value: string }) {
  const { t } = useT();
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex items-center justify-between gap-2">
      <div className="flex min-w-0 flex-col">
        <span className="text-muted-foreground text-xs">{label}</span>
        <span className="font-mono text-sm break-all">{value}</span>
      </div>
      <Button
        variant="outline"
        size="sm"
        aria-label={t('payPublic:bank.copyOf', { label })}
        onClick={() =>
          void navigator.clipboard?.writeText(value).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          })
        }
      >
        <CopyIcon />
        {copied ? t('payPublic:bank.copied') : t('payPublic:bank.copy')}
      </Button>
    </div>
  );
}

function BankCard({ bank }: { bank: PayBank }) {
  const { t } = useT();
  return (
    <Card data-testid="pay-bank">
      <CardHeader>
        <CardTitle>{t('payPublic:bank.title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <CopyLine label={t('payPublic:bank.iban')} value={bank.iban} />
        <div className="flex flex-col">
          <span className="text-muted-foreground text-xs">{t('payPublic:bank.holder')}</span>
          <span className="text-sm">{bank.holder}</span>
        </div>
        {bank.bank ? (
          <div className="flex flex-col">
            <span className="text-muted-foreground text-xs">{t('payPublic:bank.bank')}</span>
            <span className="text-sm">{bank.bank}</span>
          </div>
        ) : null}
        {bank.bic ? <CopyLine label={t('payPublic:bank.bic')} value={bank.bic} /> : null}
        <CopyLine label={t('payPublic:bank.reference')} value={bank.reference} />
        <p className="text-muted-foreground text-xs">{t('payPublic:bank.hint')}</p>
      </CardContent>
    </Card>
  );
}

function StatusAlert({ view }: { view: PayView }) {
  const { t } = useT();
  const icon =
    view === 'paid' ? <CheckCircle2Icon /> : view === 'verifying' ? <HourglassIcon /> : null;
  return (
    <Alert role="status" data-testid={`pay-${view}`}>
      {icon}
      <AlertTitle>{t(`payPublic:status.${view}.title`)}</AlertTitle>
      <AlertDescription>{t(`payPublic:status.${view}.description`)}</AlertDescription>
    </Alert>
  );
}

function ReturnView({ payRef }: { payRef: PayRef }) {
  const { t } = useT();
  const status = usePayStatus(payRef);
  if (status.isPending) return <Loading />;
  if (status.isError) return <Invalid error={status.error} />;
  const view = payView(status.data.status, status.data.verifying, true);
  return (
    <>
      <h1 className="text-2xl font-semibold">{t('payPublic:title')}</h1>
      <StatusAlert view={view} />
    </>
  );
}
