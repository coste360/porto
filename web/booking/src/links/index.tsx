import { LinkIcon } from 'lucide-react';

import { usePublicLink } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { BrandLogo } from '@porto/shared/ui/patterns/Brand';
import { EmptyState } from '@porto/shared/ui/patterns/EmptyState';
import { Skeleton } from '@porto/shared/ui/skeleton';

import type { LinkPurpose } from '../routes';
import { QuoteLinkPage } from './QuoteLinkPage';
import { RenewalLinkPage } from './RenewalLinkPage';
import { SigningPage } from './SigningPage';
import { UnsubscribePage } from './UnsubscribePage';
import { YardQuoteLinkPage } from './YardQuoteLinkPage';

// Customer magic-link pages (m4-plan §8.1): quote acceptance, renewal proposal, signing and unsubscribe. The page
// resolves the token once; an unknown, expired or revoked link answers 404 and shows the same "invalid" page.
// Phone-first (390 px): one column, large touch targets.

const TITLES: Record<LinkPurpose, string> = {
  quote: 'customerLinks:quote',
  renewal: 'customerLinks:renewal',
  signature: 'customerLinks:signature',
  unsubscribe: 'customerLinks:unsubscribe',
};

export function LinkPage({
  slug,
  purpose,
  token,
}: {
  slug: string;
  purpose: LinkPurpose;
  token: string;
}) {
  const { t } = useT();
  const link = usePublicLink({ slug, token });
  const ref = { slug, token };
  // A link of another purpose than the URL says is treated as invalid (no cross-purpose use of a token).
  // A link that loaded and then stopped resolving because of the customer's own action (a signature completes and
  // revokes it) keeps its page and the result shown on it; only a link that never loaded is invalid.
  const invalid = (link.isError && !link.data) || (link.data && link.data.purpose !== purpose);
  return (
    <div className="flex min-h-svh flex-col">
      <main className="mx-auto flex w-full max-w-md flex-1 flex-col gap-6 p-4">
        {link.data && !invalid ? (
          <p className="text-muted-foreground text-sm">{link.data.marina.name}</p>
        ) : null}
        <h1 className="text-2xl font-semibold tracking-tight">{t(TITLES[purpose])}</h1>
        {link.isPending ? (
          <div
            className="flex flex-col gap-3"
            aria-busy="true"
            aria-label={t('customerLinks:loading')}
          >
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-40 w-full" />
          </div>
        ) : invalid || !link.data ? (
          <EmptyState
            icon={<LinkIcon />}
            title={t('customerLinks:invalid')}
            description={t('customerLinks:invalidHint')}
          />
        ) : purpose === 'quote' && link.data.yard_quote ? (
          <YardQuoteLinkPage linkRef={ref} quote={link.data.yard_quote} />
        ) : purpose === 'quote' && link.data.quote ? (
          <QuoteLinkPage linkRef={ref} quote={link.data.quote} />
        ) : purpose === 'renewal' && link.data.renewal ? (
          <RenewalLinkPage linkRef={ref} renewal={link.data.renewal} />
        ) : purpose === 'signature' && link.data.signature ? (
          <SigningPage linkRef={ref} signature={link.data.signature} />
        ) : purpose === 'unsubscribe' && link.data.unsubscribe ? (
          <UnsubscribePage linkRef={ref} subscribed={link.data.unsubscribe.subscribed} />
        ) : (
          <EmptyState icon={<LinkIcon />} title={t('customerLinks:invalid')} />
        )}
      </main>
      <footer className="text-muted-foreground flex items-center justify-center gap-1 p-4 text-xs">
        <BrandLogo variant="symbol" size="sm" />
        <span>{t('common:product.poweredBy')}</span>
      </footer>
    </div>
  );
}
