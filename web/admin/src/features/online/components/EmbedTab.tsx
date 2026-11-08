import { CopyIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Textarea } from '@porto/shared/ui/textarea';

import { embedSnippets } from '../lib/constants';

/** Allowed origins (read-only, edited in the marina settings), snippet generator with copy buttons, preview. */
export function EmbedTab({ slug, origins }: { slug: string; origins: string[] }) {
  const { t, i18n } = useT();
  const lang = i18n.language.startsWith('en') ? 'en' : 'it';
  const s = embedSnippets(window.location.origin, slug, lang);
  const copy = (text: string) =>
    void navigator.clipboard
      ?.writeText(text)
      .then(() => toast.success(t('online:settings.embed.copied')))
      .catch(() => undefined);
  const snippet = (id: string, label: string, text: string, rows: number) => (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Textarea id={id} readOnly rows={rows} value={text} className="font-mono text-xs" />
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => copy(text)}
          data-testid={`${id}-copy`}
        >
          <CopyIcon />
          {t('online:settings.embed.copy')}
        </Button>
      </div>
    </Field>
  );
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('online:settings.embed.origins')}</CardTitle>
          <CardDescription>{t('online:settings.embed.originsHelp')}</CardDescription>
        </CardHeader>
        <CardContent>
          {origins.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('online:settings.embed.noOrigins')}</p>
          ) : (
            <ul className="flex flex-col gap-1 font-mono text-sm" data-testid="embed-origins">
              {origins.map((o) => (
                <li key={o}>{o}</li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardContent className="flex flex-col gap-4 pt-6">
          {snippet('embed-script', t('online:settings.embed.script'), s.script, 3)}
          {snippet('embed-iframe', t('online:settings.embed.iframe'), s.iframe, 3)}
          {snippet('embed-link', t('online:settings.embed.link'), s.link, 1)}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('online:settings.embed.preview')}</CardTitle>
        </CardHeader>
        <CardContent>
          <iframe
            src={`${s.link}?embed=1&lang=${lang}`}
            title={t('online:settings.embed.preview')}
            className="h-96 w-full rounded-md border"
            loading="lazy"
          />
        </CardContent>
      </Card>
    </div>
  );
}
