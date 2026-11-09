import { Controller, type Control, type FieldPath } from 'react-hook-form';

import { useT } from '@porto/shared/i18n';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';
import { Textarea } from '@porto/shared/ui/textarea';

import type { ProfileValues } from '../lib/profileForm';

type TextPath =
  | 'hours_note'
  | 'access_notes'
  | 'lights'
  | 'description'
  | 'services.fuel_hours'
  | `photos.${number}.alt`
  | `access_images.${number}.alt`;

/**
 * A text per language (it is the source, en optional) edited with one tab per language. Long texts use a
 * textarea and show the limit.
 */
export function I18nField({
  control,
  name,
  label,
  id,
  max,
  multiline = true,
}: {
  control: Control<ProfileValues>;
  name: TextPath;
  label: string;
  id: string;
  max: number;
  multiline?: boolean;
}) {
  const { t } = useT();
  return (
    <Field>
      <FieldLabel htmlFor={`${id}-it`}>{label}</FieldLabel>
      <Tabs defaultValue="it">
        <TabsList>
          <TabsTrigger value="it">{t('profile:lang.it')}</TabsTrigger>
          <TabsTrigger value="en">{t('profile:lang.en')}</TabsTrigger>
        </TabsList>
        {(['it', 'en'] as const).map((lang) => (
          <TabsContent key={lang} value={lang}>
            <Controller
              control={control}
              name={`${name}.${lang}` as FieldPath<ProfileValues>}
              render={({ field }) => {
                const value = typeof field.value === 'string' ? field.value : '';
                const common = {
                  id: `${id}-${lang}`,
                  value,
                  maxLength: max,
                  'aria-label': `${label} (${t(`profile:lang.${lang}`)})`,
                  onBlur: field.onBlur,
                };
                return multiline ? (
                  <Textarea {...common} rows={3} onChange={(e) => field.onChange(e.target.value)} />
                ) : (
                  <Input {...common} onChange={(e) => field.onChange(e.target.value)} />
                );
              }}
            />
          </TabsContent>
        ))}
      </Tabs>
      {multiline ? <FieldDescription>{t('profile:maxChars', { max })}</FieldDescription> : null}
    </Field>
  );
}
