import { useState } from 'react';
import { toast } from 'sonner';

import {
  useImportOpeningBalances,
  useOpeningBalanceImports,
  useUndoOpeningBalanceImport,
  type ChargeImport,
  type OpeningBalanceImportInput,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import { TableCell, TableRow } from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import {
  BillingBadge,
  BillingProblemAlert,
  EnumSelect,
  LoadMore,
  Money,
  PageFrame,
  Section,
  SimpleTable,
} from '../components/kit';
import { fileToBase64, formatOf } from '../lib/catalog';

type Mapping = OpeningBalanceImportInput['mapping'];
type Delimiter = NonNullable<OpeningBalanceImportInput['delimiter']>;

const COLUMNS = [
  'customer_column',
  'amount_column',
  'due_on_column',
  'description_column',
  'reference_column',
] as const;

/** Opening balances import (spec 11.6, m5-plan §3.8): file, mapping, dry run, apply, undo. */
export function OpeningBalancesPage() {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const run = useImportOpeningBalances(marinaId);
  const undo = useUndoOpeningBalanceImport(marinaId);
  const history = useOpeningBalanceImports(marinaId);
  const [file, setFile] = useState<{ name: string; base64: string } | null>(null);
  const [hasHeader, setHasHeader] = useState(true);
  const [delimiter, setDelimiter] = useState<Delimiter>('semicolon');
  const [mapping, setMapping] = useState<Mapping>({
    customer_match: 'fiscal_code',
    customer_column: 0,
    amount_column: 1,
    due_on_column: 2,
  });
  const [result, setResult] = useState<ChargeImport | null>(null);
  const items = history.data?.pages.flatMap((p) => p.items) ?? [];
  const submit = (dry: boolean) => {
    if (!file) return;
    const format = formatOf(file.name);
    run.mutate(
      {
        format,
        file_base64: file.base64,
        has_header: hasHeader,
        mapping,
        dry_run: dry,
        ...(format === 'csv' ? { delimiter } : {}),
      },
      {
        onSuccess: (r) => {
          setResult(r);
          if (!dry) toast.success(t('customerBilling:opening.applied', { count: r.charge_count }));
        },
      },
    );
  };
  return (
    <RequirePermission allowed={can('charges.manage')}>
      <PageFrame>
        <PageHeader
          title={t('customerBilling:opening.title')}
          description={t('customerBilling:opening.description')}
        />
        <Section title={t('customerBilling:opening.file')}>
          <div className="flex flex-wrap items-end gap-3">
            <Field className="w-72">
              <FieldLabel htmlFor="ob-file">{t('customerBilling:opening.upload')}</FieldLabel>
              <Input
                id="ob-file"
                type="file"
                accept=".csv,.xlsx,text/csv"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  setResult(null);
                  if (f) void fileToBase64(f).then((base64) => setFile({ name: f.name, base64 }));
                  else setFile(null);
                }}
              />
            </Field>
            {file && formatOf(file.name) === 'csv' ? (
              <EnumSelect
                id="ob-delimiter"
                label={t('customerBilling:opening.delimiter')}
                values={['semicolon', 'comma', 'tab'] as const}
                labelOf={(d) => t(`customerBilling:opening.delimiters.${d}`)}
                value={delimiter}
                onChange={(d) => d && setDelimiter(d)}
              />
            ) : null}
            <label className="flex items-center gap-2 pb-2 text-sm">
              <Checkbox checked={hasHeader} onCheckedChange={(v) => setHasHeader(v === true)} />
              {t('customerBilling:opening.hasHeader')}
            </label>
          </div>
        </Section>
        <Section
          title={t('customerBilling:opening.mapping')}
          description={t('customerBilling:opening.mappingHint')}
        >
          <div className="flex flex-wrap items-end gap-3">
            <EnumSelect
              id="ob-match"
              label={t('customerBilling:opening.match')}
              values={['fiscal_code', 'vat_number', 'customer_code'] as const}
              labelOf={(m) => t(`customerBilling:opening.matches.${m}`)}
              value={mapping.customer_match}
              onChange={(m) => m && setMapping({ ...mapping, customer_match: m })}
            />
            {COLUMNS.map((c) => (
              <Field key={c} className="w-36">
                <FieldLabel htmlFor={`ob-${c}`}>
                  {t(`customerBilling:opening.columns.${c}`)}
                </FieldLabel>
                <Input
                  id={`ob-${c}`}
                  type="number"
                  min={0}
                  max={200}
                  value={mapping[c] ?? ''}
                  onChange={(e) => {
                    const v = e.target.value === '' ? undefined : Number(e.target.value);
                    const next = { ...mapping } as Record<string, unknown>;
                    if (v === undefined || Number.isNaN(v)) delete next[c];
                    else next[c] = v;
                    setMapping(next as Mapping);
                  }}
                />
              </Field>
            ))}
          </div>
          <BillingProblemAlert error={run.error} />
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={!file || run.isPending}
              onClick={() => submit(true)}
            >
              {t('customerBilling:opening.dryRun')}
            </Button>
            <Button
              disabled={!file || run.isPending || result?.status !== 'dry_run'}
              onClick={() => submit(false)}
            >
              {t('customerBilling:opening.apply')}
            </Button>
          </div>
        </Section>
        {result ? (
          <Section title={t('customerBilling:opening.result')}>
            <p role="status" className="text-sm" data-testid="opening-result">
              {t('customerBilling:opening.summary', {
                valid: result.valid_rows,
                issues: result.issues.length,
              })}{' '}
              {result.total_cents !== undefined ? <Money cents={result.total_cents} /> : null}
            </p>
            <SimpleTable
              head={[t('customerBilling:opening.row'), t('customerBilling:opening.issue')]}
              empty={t('customerBilling:opening.noIssues')}
              rows={result.issues.length}
            >
              {result.issues.map((i) => (
                <TableRow key={`${i.row}-${i.code}`}>
                  <TableCell>{i.row}</TableCell>
                  <TableCell>{t(`billingEnums:importIssue.${i.code}`)}</TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          </Section>
        ) : null}
        <Section title={t('customerBilling:opening.history')}>
          {history.isPending ? (
            <LoadingState />
          ) : history.isError ? (
            <QueryErrorState error={history.error} />
          ) : (
            <SimpleTable
              head={[
                t('customerBilling:opening.createdAt'),
                t('customerBilling:opening.status'),
                t('customerBilling:opening.charges'),
                t('customerBilling:opening.total'),
                '',
              ]}
              empty={t('customerBilling:opening.empty')}
              rows={items.length}
            >
              {items.map((i) => (
                <TableRow key={i.id} data-testid="opening-import">
                  <TableCell>{formatDateTime(i.created_at, i18n.language)}</TableCell>
                  <TableCell>
                    <BillingBadge group="importStatus" value={i.status} />
                  </TableCell>
                  <TableCell>{i.charge_count}</TableCell>
                  <TableCell>
                    <Money cents={i.total_cents} />
                  </TableCell>
                  <TableCell className="text-right">
                    {i.status === 'applied' ? (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={undo.isPending}
                        onClick={() => undo.mutate({ id: i.id, version: i.version })}
                      >
                        {t('customerBilling:opening.undo')}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </SimpleTable>
          )}
          <BillingProblemAlert error={undo.error} />
          <LoadMore query={history} />
        </Section>
      </PageFrame>
    </RequirePermission>
  );
}
