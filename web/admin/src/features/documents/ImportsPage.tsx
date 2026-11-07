import { useState } from 'react';
import { toast } from 'sonner';

import {
  useImports,
  useRunImport,
  useUndoImport,
  type ImportEntity,
  type ImportReport,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldError, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import {
  EnumSelect,
  M4ProblemAlert,
  PageFrame,
  Section,
  StepBar,
} from '../contracts-m4/components/kit';
import {
  IMPORT_FIELDS,
  IMPORT_PERMISSION,
  checkCsvText,
  guessImportMapping,
  missingRequired,
  reportSummary,
  toApiMapping,
  type Delimiter,
  type Mapping,
} from './lib/imports';

const ENTITIES: readonly ImportEntity[] = ['customers_vessels', 'contracts', 'rights', 'deadlines'];
const STEPS = ['entity', 'file', 'mapping', 'report'] as const;
type Step = (typeof STEPS)[number];
const NONE = '__none__';

/** CSV imports (spec 11.6, D27): entity → upload → column mapping → dry-run report → apply; undo from history. */
export function ImportsPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const allowed = ENTITIES.filter((e) => can(IMPORT_PERMISSION[e]));
  const [step, setStep] = useState<Step>('entity');
  const [entity, setEntity] = useState<ImportEntity | ''>(allowed[0] ?? '');
  const [csv, setCsv] = useState<{
    name: string;
    text: string;
    rows: string[][];
    delimiter: Delimiter;
  } | null>(null);
  const [hasHeader, setHasHeader] = useState(true);
  const [mapping, setMapping] = useState<Mapping>({});
  const [fileError, setFileError] = useState<string | null>(null);
  const [report, setReport] = useState<ImportReport | null>(null);
  const run = useRunImport(marinaId);
  const history = useImports(marinaId);
  const undo = useUndoImport(marinaId);
  const header = csv?.rows[0] ?? [];
  const fields = entity
    ? [...IMPORT_FIELDS[entity].required, ...IMPORT_FIELDS[entity].optional]
    : [];
  const missing = entity ? missingRequired(entity, mapping) : [];
  const body = (dryRun: boolean) =>
    entity && csv
      ? {
          entity,
          csv: csv.text,
          delimiter: csv.delimiter,
          has_header: hasHeader,
          mapping: toApiMapping(mapping),
          dry_run: dryRun,
        }
      : null;
  return (
    <RequirePermission allowed={allowed.length > 0}>
      <PageFrame>
        <PageHeader title={t('imports:m4.title')} description={t('imports:m4.description')} />
        <StepBar
          steps={STEPS}
          current={step}
          label={t('imports:steps.label')}
          labelOf={(s) => t(`imports:m4.steps.${s}`)}
          onSelect={setStep}
        />
        {step === 'entity' ? (
          <div className="flex flex-col gap-3">
            <EnumSelect
              id="imp-entity"
              label={t('imports:m4.entity')}
              group="importEntity"
              values={allowed}
              value={entity}
              onChange={setEntity}
            />
            <div>
              <Button disabled={!entity} onClick={() => setStep('file')}>
                {t('renewals:wizard.next')}
              </Button>
            </div>
          </div>
        ) : null}
        {step === 'file' ? (
          <div className="flex flex-col gap-3">
            <Field data-invalid={fileError ? true : undefined}>
              <FieldLabel htmlFor="imp-file">{t('imports:file.label')}</FieldLabel>
              <Input
                id="imp-file"
                type="file"
                accept=".csv,text/csv"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  setFileError(null);
                  if (!f || !entity) return;
                  void f.text().then((text) => {
                    const c = checkCsvText(text, f.size);
                    if (!c.ok) {
                      setFileError(c.error);
                      return;
                    }
                    setCsv({ name: f.name, text, rows: c.rows, delimiter: c.delimiter });
                    setMapping(guessImportMapping(entity, c.rows[0] ?? []));
                  });
                }}
              />
              {fileError ? <FieldError>{t(`imports:file.${fileError}`)}</FieldError> : null}
            </Field>
            {csv ? (
              <p className="text-sm" role="status">
                {t('imports:file.loaded', {
                  name: csv.name,
                  count: csv.rows.length - (hasHeader ? 1 : 0),
                })}
              </p>
            ) : null}
            <Field orientation="horizontal">
              <Checkbox
                id="imp-header"
                checked={hasHeader}
                onCheckedChange={(c) => setHasHeader(c === true)}
              />
              <FieldLabel htmlFor="imp-header">{t('imports:file.hasHeader')}</FieldLabel>
            </Field>
            <div>
              <Button disabled={!csv} onClick={() => setStep('mapping')}>
                {t('renewals:wizard.next')}
              </Button>
            </div>
          </div>
        ) : null}
        {step === 'mapping' && entity && csv ? (
          <div className="flex flex-col gap-3">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {fields.map((f) => (
                <EnumSelect
                  key={f}
                  id={`imp-map-${f}`}
                  label={`${t(`imports:m4.fields.${f}`)}${IMPORT_FIELDS[entity].required.includes(f) ? ` ${t('imports:columns.requiredMark')}` : ''}`}
                  values={[NONE, ...header.map((_, i) => String(i))]}
                  labelOf={(v) =>
                    v === NONE
                      ? t('imports:columns.none')
                      : hasHeader
                        ? header[Number(v)] || t('imports:columns.columnN', { n: Number(v) + 1 })
                        : t('imports:columns.columnN', { n: Number(v) + 1 })
                  }
                  value={mapping[f] === undefined ? NONE : String(mapping[f])}
                  onChange={(v) =>
                    setMapping({ ...mapping, [f]: v === NONE || v === '' ? undefined : Number(v) })
                  }
                />
              ))}
            </div>
            {missing.length > 0 ? (
              <FieldError>
                {t('imports:m4.missing', {
                  fields: missing.map((f) => t(`imports:m4.fields.${f}`)).join(', '),
                })}
              </FieldError>
            ) : null}
            <M4ProblemAlert error={run.error} />
            <div>
              <Button
                disabled={missing.length > 0 || run.isPending}
                onClick={() => {
                  const b = body(true);
                  if (b)
                    run.mutate(b, {
                      onSuccess: (r) => {
                        setReport(r);
                        setStep('report');
                      },
                    });
                }}
              >
                {t('imports:m4.dryRun')}
              </Button>
            </div>
          </div>
        ) : null}
        {step === 'report' && report ? (
          <ReportView
            report={report}
            applying={run.isPending}
            error={run.error}
            onApply={() => {
              const b = body(false);
              if (b)
                run.mutate(b, {
                  onSuccess: (r) => {
                    setReport(r);
                    if (r.rejected.length === 0)
                      toast.success(t('imports:m4.applied', { count: r.valid }));
                  },
                });
            }}
          />
        ) : null}
        <Section title={t('imports:m4.history')}>
          <M4ProblemAlert error={undo.error} />
          {history.isPending ? (
            <LoadingState rows={3} />
          ) : history.isError ? (
            <QueryErrorState error={history.error} onRetry={() => void history.refetch()} />
          ) : history.data.items.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('imports:m4.noHistory')}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {history.data.items.map((r) => (
                <HistoryRow
                  key={r.id}
                  run={r}
                  onUndo={() => undo.mutate({ id: r.id, version: r.version })}
                  undoing={undo.isPending}
                />
              ))}
            </ul>
          )}
        </Section>
      </PageFrame>
    </RequirePermission>
  );
}

function HistoryRow({
  run,
  onUndo,
  undoing,
}: {
  run: {
    entity: ImportEntity;
    status: string;
    row_count: number;
    undoable?: boolean;
    created_at: string;
  };
  onUndo: () => void;
  undoing: boolean;
}) {
  const { t, i18n } = useT();
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-md border p-2 text-sm">
      <span>{t(`docsEnums:importEntity.${run.entity}`)}</span>
      <Badge variant="secondary">{t(`docsEnums:importStatus.${run.status}`)}</Badge>
      <span>{t('imports:m4.rows', { count: run.row_count })}</span>
      <span className="text-muted-foreground">{formatDateTime(run.created_at, i18n.language)}</span>
      {run.status === 'applied' && run.undoable ? (
        <Button size="sm" variant="outline" disabled={undoing} onClick={onUndo}>
          {t('imports:m4.undo')}
        </Button>
      ) : null}
    </li>
  );
}

function ReportView({
  report,
  applying,
  error,
  onApply,
}: {
  report: ImportReport;
  applying: boolean;
  error: unknown;
  onApply: () => void;
}) {
  const { t } = useT();
  const s = reportSummary(report);
  const issues = [
    ...report.rejected.map((i) => ({ ...i, kind: 'rejected' as const })),
    ...report.duplicates.map((i) => ({ ...i, kind: 'duplicate' as const })),
  ].sort((a, b) => a.row - b.row);
  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-2 sm:grid-cols-4" data-testid="import-summary">
        {(['rows', 'valid', 'rejected', 'duplicates'] as const).map((k) => (
          <div key={k} className="rounded-md border p-3">
            <div className="text-muted-foreground text-xs">{t(`imports:m4.summary.${k}`)}</div>
            <div className="text-lg font-semibold tabular-nums">{s[k]}</div>
          </div>
        ))}
      </div>
      {issues.length > 0 ? (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('imports:m4.row')}</TableHead>
                <TableHead>{t('imports:m4.kind')}</TableHead>
                <TableHead>{t('imports:m4.code')}</TableHead>
                <TableHead>{t('imports:m4.field')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {issues.slice(0, 500).map((i, n) => (
                <TableRow key={n}>
                  <TableCell>{i.row}</TableCell>
                  <TableCell>{t(`imports:m4.issue.${i.kind}`)}</TableCell>
                  <TableCell>{i.code}</TableCell>
                  <TableCell>{i.field ?? ''}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
      <M4ProblemAlert error={error} />
      {report.dry_run ? (
        <div className="flex flex-col gap-2">
          {s.rejected > 0 ? (
            <p className="text-muted-foreground text-sm" role="status">
              {t('imports:m4.fixRejected')}
            </p>
          ) : null}
          <div>
            <Button disabled={!s.applicable || applying} onClick={onApply}>
              {t('imports:m4.apply', { count: report.valid })}
            </Button>
          </div>
        </div>
      ) : (
        <p className="text-sm" role="status">
          {s.rejected > 0 ? t('imports:m4.failed') : t('imports:m4.done')}
        </p>
      )}
    </div>
  );
}
