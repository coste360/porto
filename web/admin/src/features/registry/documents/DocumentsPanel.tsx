import { zodResolver } from '@hookform/resolvers/zod';
import type { ColumnDef } from '@tanstack/react-table';
import { ExternalLinkIcon, FileUpIcon, MoreHorizontalIcon, PlusIcon } from 'lucide-react';
import { useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { toast } from 'sonner';

import {
  DOCUMENT_FILE_TYPES,
  useCreateDocument,
  useDeleteDocument,
  useDeleteDocumentFile,
  useDocumentFileUrl,
  useUpdateDocument,
  useUploadDocumentFile,
  type Document,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@porto/shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import {
  ConfirmDialog,
  DataTable,
  DatePicker,
  FormField,
  ProblemAlert,
} from '@porto/shared/ui/patterns';

import { useCurrentMarina } from '../../../auth/hooks';
import { toastProblem } from '../../../lib/toast';
import { SelectField, TextAreaField, TextField } from '../components/fields';
import { CUSTOMER_DOCUMENT_TYPES, VESSEL_DOCUMENT_TYPES } from '../lib/enums';
import {
  checkDocumentFile,
  documentSchema,
  documentToValues,
  valuesToDocumentInput,
  valuesToDocumentPatch,
  type DocumentFormValues,
} from './documentSchema';

export type DocumentsPanelProps = {
  owner: { type: 'customer' | 'vessel'; id: string };
  documents: Document[];
  canWrite: boolean;
  /** Today in the marina timezone (YYYY-MM-DD), to flag expired documents. */
  today?: string;
};

type DialogState = { kind: 'closed' } | { kind: 'create' } | { kind: 'edit'; doc: Document };

/** Documents of a customer or a vessel: metadata, encrypted file upload and signed-link viewing. */
export function DocumentsPanel({ owner, documents, canWrite, today }: DocumentsPanelProps) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [dialog, setDialog] = useState<DialogState>({ kind: 'closed' });
  const [uploadFor, setUploadFor] = useState<Document | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const upload = useUploadDocumentFile(marinaId);
  const fileUrl = useDocumentFileUrl(marinaId);
  const removeFile = useDeleteDocumentFile(marinaId);
  const remove = useDeleteDocument(marinaId);
  const onError = (error: unknown) => toastProblem(t, error);

  const uploadFile = (doc: Document, file: File) => {
    const check = checkDocumentFile(file);
    if (check !== 'ok') {
      toast.error(t(`documents:fileErrors.${check}`));
      return;
    }
    upload.mutate(
      { documentId: doc.id, file },
      { onSuccess: () => toast.success(t('documents:uploaded')), onError },
    );
  };

  const view = (doc: Document) => {
    // Open the tab synchronously (popup blockers), then point it at the signed link.
    const tab = window.open('', '_blank');
    fileUrl.mutate(doc.id, {
      onSuccess: ({ url }) => {
        if (tab) {
          tab.opener = null;
          tab.location.href = url;
        } else {
          window.location.assign(url);
        }
      },
      onError: (error) => {
        tab?.close();
        onError(error);
      },
    });
  };

  const columns: ColumnDef<Document, unknown>[] = [
    {
      header: t('documents:fields.type'),
      cell: ({ row }) => (
        <div className="flex flex-col">
          <span className="font-medium">{t(`documents:types.${row.original.type}`)}</span>
          {row.original.number ? (
            <span className="text-muted-foreground font-mono text-xs">{row.original.number}</span>
          ) : null}
        </div>
      ),
    },
    {
      header: t('documents:fields.expiresOn'),
      cell: ({ row }) => {
        const exp = row.original.expires_on;
        if (!exp) return <span className="text-muted-foreground">{t('documents:noExpiry')}</span>;
        const expired = today !== undefined && exp < today;
        return (
          <span className="flex items-center gap-2">
            {formatIsoDate(exp)}
            {expired ? <Badge variant="destructive">{t('documents:expired')}</Badge> : null}
          </span>
        );
      },
    },
    {
      header: t('documents:fields.file'),
      cell: ({ row }) =>
        row.original.has_file ? (
          <Button
            variant="link"
            size="sm"
            className="h-auto p-0"
            onClick={() => view(row.original)}
            disabled={fileUrl.isPending}
          >
            <ExternalLinkIcon />
            {row.original.file_name ?? t('documents:view')}
          </Button>
        ) : (
          <span className="text-muted-foreground">{t('documents:noFile')}</span>
        ),
    },
    {
      id: 'actions',
      header: () => <span className="sr-only">{t('common:actions.actions')}</span>,
      cell: ({ row }) =>
        canWrite ? (
          <div className="flex justify-end gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={upload.isPending}
              onClick={() => {
                setUploadFor(row.original);
                fileInput.current?.click();
              }}
            >
              <FileUpIcon />
              {row.original.has_file ? t('documents:replace') : t('documents:upload')}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={t('common:actions.actions')}>
                  <MoreHorizontalIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => setDialog({ kind: 'edit', doc: row.original })}>
                  {t('common:actions.edit')}
                </DropdownMenuItem>
                {row.original.has_file ? (
                  <DropdownMenuItem
                    onSelect={() =>
                      removeFile.mutate(row.original.id, {
                        onSuccess: () => toast.success(t('documents:fileRemoved')),
                        onError,
                      })
                    }
                  >
                    {t('documents:removeFile')}
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
            <ConfirmDialog
              trigger={
                <Button variant="ghost" size="sm">
                  {t('common:actions.delete')}
                </Button>
              }
              title={t('documents:deleteTitle')}
              description={t('documents:deleteDescription')}
              confirmLabel={t('common:actions.delete')}
              cancelLabel={t('common:actions.cancel')}
              destructive
              onConfirm={() =>
                remove.mutate(
                  { documentId: row.original.id, version: row.original.version },
                  { onSuccess: () => toast.success(t('documents:deleted')), onError },
                )
              }
            />
          </div>
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-3">
      {canWrite ? (
        <div className="flex justify-end">
          <Button variant="outline" onClick={() => setDialog({ kind: 'create' })}>
            <PlusIcon />
            {t('documents:add')}
          </Button>
        </div>
      ) : null}
      <DataTable
        columns={columns}
        data={documents}
        getRowId={(d) => d.id}
        empty={t('documents:empty')}
      />
      <input
        ref={fileInput}
        type="file"
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        accept={DOCUMENT_FILE_TYPES.join(',')}
        onChange={(e) => {
          const file = e.target.files?.[0];
          e.target.value = '';
          if (file && uploadFor) uploadFile(uploadFor, file);
        }}
      />
      <Dialog
        open={dialog.kind !== 'closed'}
        onOpenChange={(o) => (o ? null : setDialog({ kind: 'closed' }))}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {dialog.kind === 'edit' ? t('documents:editTitle') : t('documents:createTitle')}
            </DialogTitle>
          </DialogHeader>
          {dialog.kind !== 'closed' ? (
            <DocumentForm
              owner={owner}
              doc={dialog.kind === 'edit' ? dialog.doc : undefined}
              onDone={() => setDialog({ kind: 'closed' })}
              onUpload={uploadFile}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function DocumentForm({
  owner,
  doc,
  onDone,
  onUpload,
}: {
  owner: DocumentsPanelProps['owner'];
  doc?: Document;
  onDone: () => void;
  onUpload: (doc: Document, file: File) => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const create = useCreateDocument(marinaId);
  const update = useUpdateDocument(marinaId);
  const [file, setFile] = useState<File | null>(null);
  const types = owner.type === 'customer' ? CUSTOMER_DOCUMENT_TYPES : VESSEL_DOCUMENT_TYPES;
  const form = useForm<DocumentFormValues>({
    resolver: zodResolver(documentSchema),
    defaultValues: documentToValues(doc, types[0]),
  });
  const mutation = doc ? update : create;
  const c = form.control;

  const submit = form.handleSubmit((values) => {
    if (doc) {
      update.mutate(
        { documentId: doc.id, version: doc.version, patch: valuesToDocumentPatch(values) },
        {
          onSuccess: () => {
            toast.success(t('documents:saved'));
            onDone();
          },
        },
      );
      return;
    }
    create.mutate(valuesToDocumentInput(values, owner), {
      onSuccess: (created) => {
        toast.success(t('documents:created'));
        if (file) onUpload(created, file);
        onDone();
      },
    });
  });

  // An edit may target a type of the other owner list (legacy data): keep it selectable.
  const options = Array.from(new Set([...types, ...(doc ? [doc.type] : [])])).map((v) => ({
    value: v,
    label: t(`documents:types.${v}`),
  }));

  return (
    <form noValidate className="flex flex-col gap-4" onSubmit={submit}>
      {mutation.isError ? <ProblemAlert error={mutation.error} /> : null}
      <FieldGroup className="grid gap-4 sm:grid-cols-2">
        <SelectField control={c} name="type" label={t('documents:fields.type')} options={options} />
        <TextField control={c} name="number" label={t('documents:fields.number')} />
        <TextField control={c} name="issuer" label={t('documents:fields.issuer')} />
        <FormField
          control={c}
          name="issued_on"
          label={t('documents:fields.issuedOn')}
          render={({ value, onChange, id, ...rest }) => (
            <DatePicker
              id={id}
              aria-invalid={rest['aria-invalid']}
              value={value || null}
              onChange={(d) => onChange(d ?? '')}
              placeholder={t('documents:pickDate')}
            />
          )}
        />
        <FormField
          control={c}
          name="expires_on"
          label={t('documents:fields.expiresOn')}
          render={({ value, onChange, id, ...rest }) => (
            <DatePicker
              id={id}
              aria-invalid={rest['aria-invalid']}
              value={value || null}
              onChange={(d) => onChange(d ?? '')}
              placeholder={t('documents:pickDate')}
            />
          )}
        />
      </FieldGroup>
      <TextAreaField control={c} name="notes" label={t('documents:fields.notes')} />
      {doc ? null : (
        <Field>
          <FieldLabel htmlFor="document-file">{t('documents:fields.file')}</FieldLabel>
          <Input
            id="document-file"
            type="file"
            accept={DOCUMENT_FILE_TYPES.join(',')}
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
          <FieldDescription>{t('documents:fileHint')}</FieldDescription>
        </Field>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onDone}>
          {t('common:actions.cancel')}
        </Button>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? t('common:actions.saving') : t('common:actions.save')}
        </Button>
      </div>
    </form>
  );
}
