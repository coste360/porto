import { BellIcon, CheckIcon, UploadIcon, XIcon } from 'lucide-react';
import { useRef, useState } from 'react';

import {
  PAPER_SCAN_MAX_BYTES,
  PAPER_SCAN_TYPES,
  useConfirmPaperSignature,
  useSignatureAction,
  useSignatureRequest,
  useUploadPaperScan,
  type SignatureRequest,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldDescription, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { M4ProblemAlert } from '../../contracts-m4/components/kit';
import { signatureVariant, signerTimeline } from '../lib/signature';

const OPEN: readonly SignatureRequest['status'][] = [
  'pending_document',
  'sent',
  'partially_signed',
];

/** Signature request of a subject: status, signers timeline, reminders, cancel and the paper path. */
export function SignaturePanel({ requestId }: { requestId: string }) {
  const { marinaId } = useCurrentMarina();
  const req = useSignatureRequest(marinaId, requestId);
  if (req.isPending) return <LoadingState rows={2} />;
  if (req.isError) return <QueryErrorState error={req.error} onRetry={() => void req.refetch()} />;
  return <SignatureDetail request={req.data} />;
}

export function SignatureDetail({ request }: { request: SignatureRequest }) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const action = useSignatureAction(marinaId);
  const write = can('contracts.write');
  const open = OPEN.includes(request.status);
  return (
    <div className="flex flex-col gap-4" data-testid="signature-request">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant={signatureVariant(request.status)}>
          {t(`docsEnums:signatureStatus.${request.status}`)}
        </Badge>
        <span>{t(`docsEnums:signatureDriver.${request.driver}`)}</span>
        <span>{t(`docsEnums:orderMode.${request.order_mode}`)}</span>
        <span className="text-muted-foreground">
          {t('signature:expiresAt', { at: formatDateTime(request.expires_at, i18n.language) })}
        </span>
        {request.driver === 'otp' ? (
          <span className="text-muted-foreground">{t('signature:simpleSignature')}</span>
        ) : null}
      </div>
      <ol className="flex flex-col gap-3">
        {request.signers.map((s) => (
          <li key={s.id} className="rounded-md border p-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{`${s.seq}. ${s.display_name ?? t(`docsEnums:signerRole.${s.role}`)}`}</span>
              <Badge variant="outline">{t(`docsEnums:signerRole.${s.role}`)}</Badge>
              <Badge
                variant={
                  s.status === 'signed'
                    ? 'default'
                    : s.status === 'declined'
                      ? 'destructive'
                      : 'secondary'
                }
              >
                {t(`docsEnums:signerStatus.${s.status}`)}
              </Badge>
              {s.has_paper_scan ? (
                <Badge variant="outline">{t('signature:paper.scanned')}</Badge>
              ) : null}
            </div>
            <ul className="text-muted-foreground mt-1 flex flex-wrap gap-3 text-xs">
              {signerTimeline(s).map((e) => (
                <li key={e.key}>
                  {t(`signature:timeline.${e.key}`, { at: formatDateTime(e.at, i18n.language) })}
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ol>
      <M4ProblemAlert error={action.error} />
      {write && open ? (
        <div className="flex flex-wrap gap-2">
          {request.driver !== 'paper' && request.status !== 'pending_document' ? (
            <Button
              variant="outline"
              disabled={action.isPending}
              onClick={() =>
                action.mutate({ id: request.id, version: request.version, action: 'remind' })
              }
            >
              <BellIcon />
              {t('signature:remind')}
            </Button>
          ) : null}
          <Button
            variant="outline"
            disabled={action.isPending}
            onClick={() =>
              action.mutate({ id: request.id, version: request.version, action: 'cancel' })
            }
          >
            <XIcon />
            {t('signature:cancel')}
          </Button>
        </div>
      ) : null}
      {write && open && request.status !== 'pending_document' ? (
        <PaperSignature request={request} />
      ) : null}
    </div>
  );
}

/** Paper path (D11): upload the scan of the signed print, then confirm "firmato su carta". */
function PaperSignature({ request }: { request: SignatureRequest }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const upload = useUploadPaperScan(marinaId);
  const confirm = useConfirmPaperSignature(marinaId);
  const input = useRef<HTMLInputElement>(null);
  const [fileId, setFileId] = useState<string | null>(null);
  const [fileError, setFileError] = useState<'type' | 'size' | null>(null);
  return (
    <section className="flex flex-col gap-3 rounded-md border p-3" aria-labelledby="paper-title">
      <h3 id="paper-title" className="text-sm font-medium">
        {t('signature:paper.title')}
      </h3>
      <Field data-invalid={fileError ? true : undefined}>
        <FieldLabel htmlFor="paper-scan">{t('signature:paper.scan')}</FieldLabel>
        <Input
          ref={input}
          id="paper-scan"
          type="file"
          accept={PAPER_SCAN_TYPES.join(',')}
          onChange={(e) => {
            const f = e.target.files?.[0];
            setFileError(null);
            if (!f) return;
            if (!(PAPER_SCAN_TYPES as readonly string[]).includes(f.type))
              return setFileError('type');
            if (f.size > PAPER_SCAN_MAX_BYTES) return setFileError('size');
            upload.mutate(
              { requestId: request.id, file: f },
              { onSuccess: (r) => setFileId(r.file_id) },
            );
          }}
        />
        <FieldDescription>{t('signature:paper.hint')}</FieldDescription>
        {fileError ? <FieldError>{t(`signature:paper.errors.${fileError}`)}</FieldError> : null}
      </Field>
      {fileId ? (
        <p className="flex items-center gap-1 text-sm">
          <UploadIcon className="size-4" />
          {t('signature:paper.uploaded')}
        </p>
      ) : null}
      <M4ProblemAlert error={upload.error ?? confirm.error} />
      <div>
        <Button
          disabled={confirm.isPending || upload.isPending}
          onClick={() =>
            confirm.mutate({
              id: request.id,
              version: request.version,
              body: fileId ? { paper_scan_file_id: fileId } : {},
            })
          }
        >
          <CheckIcon />
          {t('signature:paper.confirm')}
        </Button>
      </div>
    </section>
  );
}
