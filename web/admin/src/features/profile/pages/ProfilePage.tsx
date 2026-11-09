import { CopyIcon, ExternalLinkIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useLocalMode, useMarinaProfile, usePublishMarinaProfile } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent } from '@porto/shared/ui/card';
import { PageHeader } from '@porto/shared/ui/patterns';
import { LocalModeNotice } from '@porto/shared/ui/patterns/LocalModeNotice';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { PageFrame } from '../../contracts-m4/components/kit';
import { ProfileForm } from '../components/ProfileForm';
import { canPublish, previewUrl } from '../lib/profileForm';

/**
 * Scheda pubblica (m12-plan §16.2): the draft form, the public URL, the preview of the draft on /p/{slug} and the
 * publication. Publishing needs a position, a contact and a name; the missing items are listed.
 */
export function ProfilePage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const profile = useMarinaProfile(marinaId);
  const publish = usePublishMarinaProfile(marinaId);
  const localMode = useLocalMode();
  const p = profile.data;
  return (
    <RequirePermission allowed={can('marina.configure')}>
      <PageFrame>
        <PageHeader title={t('profile:title')} description={t('profile:description')} />
        {profile.isPending ? (
          <LoadingState rows={6} />
        ) : profile.isError || !p ? (
          <QueryErrorState error={profile.error} onRetry={() => void profile.refetch()} />
        ) : (
          <div className="flex flex-col gap-4">
            <LocalModeNotice localMode={localMode} feature="publicPage" />
            <Card>
              <CardContent className="flex flex-col gap-3 pt-6">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge
                    variant={p.published ? 'default' : 'secondary'}
                    data-testid="profile-status"
                  >
                    {p.published ? t('profile:published') : t('profile:draft')}
                  </Badge>
                  <span className="text-muted-foreground text-sm">{t('profile:publicUrl')}</span>
                  <code
                    className="bg-muted rounded-sm px-2 py-1 text-sm break-all"
                    data-testid="profile-url"
                  >
                    {p.public_url}
                  </code>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t('profile:copy')}
                    onClick={() =>
                      void navigator.clipboard
                        ?.writeText(p.public_url)
                        .then(() => toast.success(t('profile:copied')))
                    }
                  >
                    <CopyIcon />
                  </Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button asChild variant="outline">
                    <a
                      href={previewUrl(p.public_url, marinaId)}
                      target="_blank"
                      rel="noopener"
                      data-testid="profile-preview"
                    >
                      <ExternalLinkIcon />
                      {t('profile:preview')}
                    </a>
                  </Button>
                  {p.published ? (
                    <Button
                      type="button"
                      variant="outline"
                      disabled={publish.isPending}
                      data-testid="profile-unpublish"
                      onClick={() => publish.mutate({ version: p.version, publish: false })}
                    >
                      {t('profile:unpublish')}
                    </Button>
                  ) : (
                    <Button
                      type="button"
                      disabled={publish.isPending || !canPublish(p)}
                      data-testid="profile-publish"
                      onClick={() =>
                        publish.mutate(
                          { version: p.version, publish: true },
                          { onSuccess: () => toast.success(t('profile:publishedToast')) },
                        )
                      }
                    >
                      {t('profile:publish')}
                    </Button>
                  )}
                </div>
                {(p.missing ?? []).length > 0 ? (
                  <Alert data-testid="profile-missing">
                    <AlertTitle>{t('profile:missingTitle')}</AlertTitle>
                    <AlertDescription>
                      <ul className="list-disc pl-4">
                        {(p.missing ?? []).map((m) => (
                          <li key={m}>{t(`profile:missing.${m}`)}</li>
                        ))}
                      </ul>
                    </AlertDescription>
                  </Alert>
                ) : null}
                <ProblemAlert error={publish.error} />
              </CardContent>
            </Card>
            <ProfileForm marinaId={marinaId} profile={p} />
          </div>
        )}
      </PageFrame>
    </RequirePermission>
  );
}
