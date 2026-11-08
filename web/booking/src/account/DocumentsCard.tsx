import { BadgeCheckIcon, ClockIcon, ExternalLinkIcon, FileUpIcon, FilesIcon } from 'lucide-react';
import { useRef, useState } from 'react';

import {
  useAccountDocumentURL,
  useUploadAccountDocument,
  type AccountBooking,
  type BookingDocumentType,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Field, FieldError, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Spinner } from '@porto/shared/ui/spinner';

import { ACCOUNT_DOCUMENT_TYPES, fileProblem } from './documents';

/** Documents of the booking (type, uploaded, verified by the office) with upload from the phone camera. */
export function DocumentsCard({
  slug,
  session,
  booking: b,
}: {
  slug: string;
  session: string;
  booking: AccountBooking;
}) {
  const { t } = useT();
  const upload = useUploadAccountDocument(slug, session, b.contract_id);
  const docURL = useAccountDocumentURL(slug, session);
  const [type, setType] = useState<BookingDocumentType>(
    b.missing_documents[0] ?? 'insurance_policy',
  );
  const [fileError, setFileError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const canUpload = !['cancelled', 'expired', 'departed', 'no_show'].includes(b.status);

  const onFile = (file: File | undefined) => {
    setDone(false);
    if (!file) return;
    const problem = fileProblem(file);
    setFileError(problem);
    if (problem) return;
    upload.mutate(
      { file, type },
      {
        onSuccess: () => {
          setDone(true);
          if (input.current) input.current.value = '';
        },
      },
    );
  };
  const openDocument = (id: string) =>
    docURL.mutate(id, { onSuccess: (r) => window.open(r.url, '_blank', 'noopener') });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <FilesIcon className="size-5" aria-hidden />
          {t('account:documents.title')}
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {b.missing_documents.length > 0 ? (
          <Alert data-testid="account-missing-documents">
            <AlertDescription>
              {t('account:documents.missing', {
                list: b.missing_documents.map((d) => t(`account:documentTypes.${d}`)).join(', '),
              })}
            </AlertDescription>
          </Alert>
        ) : null}
        {b.documents.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t('account:documents.empty')}</p>
        ) : (
          <ul className="flex flex-col gap-2" data-testid="account-documents">
            {b.documents.map((d) => (
              <li key={d.id} className="flex flex-wrap items-center justify-between gap-2 text-sm">
                <span>
                  {t(`account:documentTypes.${d.type}`)} ·{' '}
                  {formatIsoDate(d.uploaded_at.slice(0, 10))}
                </span>
                <span className="flex items-center gap-2">
                  {d.verified ? (
                    <Badge>
                      <BadgeCheckIcon />
                      {t('account:documents.verified')}
                    </Badge>
                  ) : (
                    <Badge variant="outline">
                      <ClockIcon />
                      {t('account:documents.toVerify')}
                    </Badge>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => openDocument(d.id)}
                    disabled={docURL.isPending}
                  >
                    <ExternalLinkIcon />
                    {t('account:documents.view')}
                  </Button>
                </span>
              </li>
            ))}
          </ul>
        )}
        <ProblemAlert error={docURL.error} />
        {canUpload ? (
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="account-doc-type">{t('account:documents.type')}</FieldLabel>
              <Select value={type} onValueChange={(v) => setType(v as BookingDocumentType)}>
                <SelectTrigger id="account-doc-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ACCOUNT_DOCUMENT_TYPES.map((d) => (
                    <SelectItem key={d} value={d}>
                      {t(`account:documentTypes.${d}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field data-invalid={Boolean(fileError)}>
              <FieldLabel htmlFor="account-doc-file">{t('account:documents.file')}</FieldLabel>
              <Input
                id="account-doc-file"
                ref={input}
                type="file"
                accept="image/*,application/pdf"
                disabled={upload.isPending}
                onChange={(e) => onFile(e.target.files?.[0])}
                data-testid="account-doc-input"
              />
              {fileError ? <FieldError>{t(fileError)}</FieldError> : null}
            </Field>
            {upload.isPending ? (
              <p className="flex items-center gap-2 text-sm" role="status">
                <Spinner />
                <FileUpIcon className="size-4" aria-hidden />
              </p>
            ) : null}
            {done ? (
              <p className="text-sm" role="status">
                {t('account:documents.uploaded')}
              </p>
            ) : null}
            <ProblemAlert error={upload.error} />
          </FieldGroup>
        ) : null}
      </CardContent>
    </Card>
  );
}
