import { zodResolver } from '@hookform/resolvers/zod';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';
import { z } from 'zod';

import { useBookingTexts, usePublishBookingText, type BookingTextKind } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Skeleton } from '@porto/shared/ui/skeleton';
import { Textarea } from '@porto/shared/ui/textarea';

import { TEXT_KINDS } from '../lib/constants';

const schema = z.object({
  it: z.string().trim().min(1, 'common:validation.required').max(100000),
  en: z.string().trim().max(100000),
});
type Values = z.infer<typeof schema>;

/** Versioned texts (terms, privacy, marketing): published versions are immutable, each edit is a new version. */
export function TextsTab({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const [kind, setKind] = useState<BookingTextKind>('terms');
  const texts = useBookingTexts(marinaId, kind);
  const publish = usePublishBookingText(marinaId);
  const current = texts.data?.items[0];
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    values: { it: current?.texts.it ?? '', en: current?.texts.en ?? '' },
  });
  const submit = form.handleSubmit((v) =>
    publish.mutate(
      { kind, texts: { it: v.it, ...(v.en ? { en: v.en } : {}) } },
      { onSuccess: () => toast.success(t('online:settings.texts.published')) },
    ),
  );
  const err = form.formState.errors;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('online:settings.tabs.texts')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert>
          <AlertDescription>{t('online:settings.texts.immutable')}</AlertDescription>
        </Alert>
        <Field className="sm:max-w-72">
          <FieldLabel htmlFor="texts-kind">{t('online:settings.texts.kind')}</FieldLabel>
          <Select value={kind} onValueChange={(v) => setKind(v as BookingTextKind)}>
            <SelectTrigger id="texts-kind" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TEXT_KINDS.map((k) => (
                <SelectItem key={k} value={k}>
                  {t(`online:settings.textKind.${k}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        {texts.isPending ? (
          <Skeleton className="h-8 w-full" />
        ) : texts.isError ? (
          <ProblemAlert error={texts.error} />
        ) : (
          <p className="text-muted-foreground text-sm" data-testid="texts-current">
            {current
              ? t('online:settings.texts.current', {
                  version: current.version,
                  at: formatDateTime(current.published_at, i18n.language),
                })
              : t('online:settings.texts.none')}
          </p>
        )}
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <FieldGroup>
            <Field data-invalid={Boolean(err.it)}>
              <FieldLabel htmlFor="texts-it">{t('online:settings.texts.textIt')}</FieldLabel>
              <Textarea id="texts-it" rows={8} {...form.register('it')} />
              {err.it?.message ? <FieldError>{t(err.it.message)}</FieldError> : null}
            </Field>
            <Field>
              <FieldLabel htmlFor="texts-en">{t('online:settings.texts.textEn')}</FieldLabel>
              <Textarea id="texts-en" rows={8} {...form.register('en')} />
            </Field>
          </FieldGroup>
          <ProblemAlert error={publish.error} />
          <div>
            <Button type="submit" disabled={publish.isPending} data-testid="texts-publish">
              {t('online:settings.texts.publish')}
            </Button>
          </div>
        </form>
        {texts.data && texts.data.items.length > 1 ? (
          <section className="flex flex-col gap-1">
            <h3 className="font-medium">{t('online:settings.texts.history')}</h3>
            <ul className="text-muted-foreground text-sm">
              {texts.data.items.slice(1).map((x) => (
                <li key={x.id}>
                  {`v${x.version} · ${formatDateTime(x.published_at, i18n.language)}`}
                  {x.published_by ? ` · ${x.published_by.name}` : ''}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </CardContent>
    </Card>
  );
}
