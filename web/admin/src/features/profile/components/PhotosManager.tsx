import { ArrowDownIcon, ArrowUpIcon, ImagePlusIcon, TrashIcon } from 'lucide-react';
import { useId } from 'react';
import { useFieldArray, type Control } from 'react-hook-form';

import { profilePhotoUrl, useUploadProfilePhoto } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { FieldDescription, FieldLegend, FieldSet } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';

import type { ProfileValues } from '../lib/profileForm';
import { I18nField } from './I18nField';

/**
 * Photos of the profile (or the access images): upload (JPEG/PNG, re-encoded by the server without metadata), alt
 * texts per language, order, removal. The list is saved with the form.
 */
export function PhotosManager({
  marinaId,
  control,
  name,
  legend,
  max,
}: {
  marinaId: string;
  control: Control<ProfileValues>;
  name: 'photos' | 'access_images';
  legend: string;
  max: number;
}) {
  const { t } = useT();
  const inputId = useId();
  const rows = useFieldArray({ control, name });
  const upload = useUploadProfilePhoto(marinaId);
  return (
    <FieldSet>
      <FieldLegend>{legend}</FieldLegend>
      <FieldDescription>{t('profile:photos.hint', { max })}</FieldDescription>
      <ul className="grid gap-4 sm:grid-cols-2" data-testid={`profile-${name}`}>
        {rows.fields.map((row, i) => (
          <li key={row.id} className="flex flex-col gap-2 rounded-md border p-3">
            <img
              src={profilePhotoUrl(marinaId, row.file_id)}
              alt={row.alt.it || t('profile:photos.thumbnail', { n: i + 1 })}
              className="aspect-video w-full rounded-sm object-cover"
            />
            <I18nField
              control={control}
              name={`${name}.${i}.alt`}
              id={`${name}-${i}-alt`}
              label={t('profile:photos.alt')}
              max={200}
              multiline={false}
            />
            <div className="flex gap-1">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                disabled={i === 0}
                aria-label={t('profile:photos.up')}
                onClick={() => rows.move(i, i - 1)}
              >
                <ArrowUpIcon />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                disabled={i === rows.fields.length - 1}
                aria-label={t('profile:photos.down')}
                onClick={() => rows.move(i, i + 1)}
              >
                <ArrowDownIcon />
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={t('profile:photos.remove')}
                onClick={() => rows.remove(i)}
              >
                <TrashIcon />
              </Button>
            </div>
          </li>
        ))}
      </ul>
      <div className="flex flex-col gap-2">
        <label htmlFor={inputId} className="sr-only">
          {t('profile:photos.upload')}
        </label>
        <Input
          id={inputId}
          type="file"
          accept="image/jpeg,image/png"
          disabled={rows.fields.length >= max || upload.isPending}
          data-testid={`profile-${name}-upload`}
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = '';
            if (!file) return;
            upload.mutate(file, {
              onSuccess: (f) => rows.append({ file_id: f.file_id, alt: { it: '', en: '' } }),
            });
          }}
        />
        <p className="text-muted-foreground flex items-center gap-2 text-sm">
          <ImagePlusIcon className="size-4" />
          {t('profile:photos.upload')}
        </p>
        <ProblemAlert error={upload.error} />
      </div>
    </FieldSet>
  );
}
