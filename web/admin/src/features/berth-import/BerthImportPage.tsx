import { useQueryClient } from '@tanstack/react-query';
import { getRouteApi, Link } from '@tanstack/react-router';
import { CheckCircle2Icon, DownloadIcon, FileTextIcon, TriangleAlertIcon } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import {
  isProblem,
  queryKeys,
  useImportBerths,
  useImportTargetMap,
  type MapDocument,
  type Schemas,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Checkbox } from '@porto/shared/ui/checkbox';
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader, ProblemAlert } from '@porto/shared/ui/patterns';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';
import { ScrollArea } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
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
  CSV_MAX_BYTES,
  CSV_MAX_ROWS,
  MAPPING_FIELDS,
  REQUIRED_FIELDS,
  detectDelimiter,
  guessMapping,
  mappingFromNames,
  mappingToNames,
  parseCsv,
  piersInFile,
  reportToCsv,
  toContractMapping,
  type ColumnMapping,
  type Delimiter,
  type MappingField,
} from './csv';
import { downloadBlob } from '../registry/lib/format';
import { loadSavedMapping, saveMapping } from './storage';

const routeApi = getRouteApi('/auth/m/$marinaId/maps/$mapId/import-berths');

const PREVIEW_ROWS = 20;
const NONE = '__none__';

type Step = 'file' | 'columns' | 'placement' | 'report';
type Placement = Schemas['BerthImportPlacement'];
type LoadedFile = { name: string; text: string };

export function BerthImportPage() {
  const { can, marinaId } = useCurrentMarina();
  const { mapId } = routeApi.useParams();
  const map = useImportTargetMap(marinaId, mapId);
  return (
    <RequirePermission allowed={can('harbor.configure')}>
      {map.isPending ? (
        <LoadingState rows={4} />
      ) : map.isError ? (
        <QueryErrorState error={map.error} onRetry={() => void map.refetch()} />
      ) : (
        <Wizard map={map.data} />
      )}
    </RequirePermission>
  );
}

function mapName(map: MapDocument, lang: string): string {
  const name = map.name as Record<string, string | undefined>;
  return name[lang] ?? name.it ?? Object.values(name).find(Boolean) ?? '';
}

function Wizard({ map }: { map: MapDocument }) {
  const { t, i18n } = useT();
  const { marinaId } = useCurrentMarina();
  const queryClient = useQueryClient();
  const importBerths = useImportBerths(marinaId);
  const [step, setStep] = useState<Step>('file');
  const [file, setFile] = useState<LoadedFile | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [delimiter, setDelimiter] = useState<Delimiter>('comma');
  const [hasHeader, setHasHeader] = useState(true);
  const [units, setUnits] = useState<'m' | 'cm'>('m');
  const [mapping, setMapping] = useState<ColumnMapping>({});
  const [placement, setPlacement] = useState<Placement>({
    start: 'after_existing',
    side_policy: 'alternate',
  });
  const [report, setReport] = useState<Schemas['BerthImportReport'] | null>(null);
  const [applied, setApplied] = useState(false);

  const rows = file ? parseCsv(file.text, delimiter) : [];
  const header = hasHeader ? (rows[0] ?? []) : [];
  const data = hasHeader ? rows.slice(1) : rows;
  const columnCount = Math.max(0, ...rows.slice(0, PREVIEW_ROWS + 1).map((r) => r.length));
  const columnName = (i: number) => header[i]?.trim() || t('imports:columns.columnN', { n: i + 1 });
  const contractMapping = toContractMapping(mapping);
  const mapPiers = map.structures
    .filter((s) => s.kind === 'pier' || s.kind === 'quay')
    .map((s) => s.code);
  const filePiers = piersInFile(data, mapping);

  const readFile = async (f: File) => {
    setFileError(null);
    setReport(null);
    if (f.size > CSV_MAX_BYTES) return setFileError('imports:file.tooLarge');
    const text = await f.text();
    const d = detectDelimiter(text);
    const parsed = parseCsv(text, d);
    if (parsed.length === 0) return setFileError('imports:file.empty');
    if (parsed.length > CSV_MAX_ROWS + 1) return setFileError('imports:file.tooManyRows');
    setFile({ name: f.name, text });
    setDelimiter(d);
    const saved = loadSavedMapping(marinaId);
    const first = parsed[0] ?? [];
    const fromSaved = saved ? mappingFromNames(first, saved) : {};
    const guessed = guessMapping(first);
    setMapping({ ...guessed, ...fromSaved });
  };

  const request = (dryRun: boolean): Schemas['BerthImportRequest'] => ({
    csv: file!.text,
    delimiter,
    has_header: hasHeader,
    mapping: contractMapping!,
    units,
    placement,
    dry_run: dryRun,
  });

  const runDry = () => {
    if (hasHeader) saveMapping(marinaId, mappingToNames(header, mapping));
    importBerths.mutate(
      { mapId: map.id, request: request(true) },
      {
        onSuccess: (r) => {
          setReport(r);
          setStep('report');
        },
      },
    );
  };

  const apply = () =>
    importBerths.mutate(
      { mapId: map.id, request: request(false), version: map.version },
      {
        onSuccess: (r) => {
          setReport(r);
          setApplied(true);
          toast.success(t('imports:run.applied', { count: r.planned.length }));
        },
      },
    );

  const reload = () => {
    importBerths.reset();
    setReport(null);
    setStep('placement');
    void queryClient.invalidateQueries({ queryKey: queryKeys.map(marinaId, map.id) });
  };

  const steps: Step[] = ['file', 'columns', 'placement', 'report'];

  return (
    <>
      <PageHeader
        title={t('imports:title')}
        description={t('imports:description', { map: mapName(map, i18n.language) })}
      />
      <ol className="mb-6 flex flex-wrap gap-2" aria-label={t('imports:steps.label')}>
        {steps.map((s, i) => (
          <li key={s}>
            <Badge
              variant={s === step ? 'default' : 'outline'}
              aria-current={s === step ? 'step' : undefined}
            >
              {`${i + 1}. ${t(`imports:steps.${s}`)}`}
            </Badge>
          </li>
        ))}
      </ol>

      {step === 'file' ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('imports:steps.file')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Field>
              <FieldLabel htmlFor="import-file">{t('imports:file.label')}</FieldLabel>
              <Input
                id="import-file"
                type="file"
                accept=".csv,text/csv,text/plain"
                onChange={(e) => {
                  const f = e.target.files?.[0];
                  if (f) void readFile(f);
                }}
              />
              <FieldDescription>{t('imports:file.hint')}</FieldDescription>
            </Field>
            {fileError ? (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertDescription>{t(fileError)}</AlertDescription>
              </Alert>
            ) : null}
            {file ? (
              <>
                <p className="flex items-center gap-2 text-sm">
                  <FileTextIcon className="size-4" />
                  {t('imports:file.loaded', { name: file.name, count: data.length })}
                </p>
                <FieldGroup className="grid gap-4 sm:grid-cols-3">
                  <Field>
                    <FieldLabel htmlFor="import-delimiter">
                      {t('imports:file.delimiter')}
                    </FieldLabel>
                    <Select value={delimiter} onValueChange={(v) => setDelimiter(v as Delimiter)}>
                      <SelectTrigger id="import-delimiter" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {(['comma', 'semicolon', 'tab'] as const).map((d) => (
                          <SelectItem key={d} value={d}>
                            {t(`imports:file.delimiters.${d}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="import-units">{t('imports:file.units')}</FieldLabel>
                    <Select value={units} onValueChange={(v) => setUnits(v as 'm' | 'cm')}>
                      <SelectTrigger id="import-units" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="m">{t('imports:file.unitsM')}</SelectItem>
                        <SelectItem value="cm">{t('imports:file.unitsCm')}</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field orientation="horizontal" className="sm:pt-6">
                    <Checkbox
                      id="import-header"
                      checked={hasHeader}
                      onCheckedChange={(v) => setHasHeader(v === true)}
                    />
                    <FieldLabel htmlFor="import-header">{t('imports:file.hasHeader')}</FieldLabel>
                  </Field>
                </FieldGroup>
              </>
            ) : null}
            <div className="flex justify-between">
              <Button asChild variant="outline">
                <Link to="/m/$marinaId/maps/$mapId" params={{ marinaId, mapId: map.id }}>
                  {t('common:actions.back')}
                </Link>
              </Button>
              <Button disabled={!file || data.length === 0} onClick={() => setStep('columns')}>
                {t('common:actions.continue')}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {step === 'columns' && file ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('imports:steps.columns')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <FieldGroup className="grid gap-4 sm:grid-cols-4">
              {MAPPING_FIELDS.map((f) => (
                <MappingSelect
                  key={f}
                  field={f}
                  value={mapping[f]}
                  columnCount={columnCount}
                  columnName={columnName}
                  onChange={(idx) =>
                    setMapping((prev) => {
                      const next = { ...prev };
                      if (idx === undefined) delete next[f];
                      else next[f] = idx;
                      return next;
                    })
                  }
                />
              ))}
            </FieldGroup>
            {contractMapping === null ? (
              <p className="text-destructive text-sm" role="status">
                {t('imports:columns.missing')}
              </p>
            ) : null}
            <h3 className="text-sm font-medium">
              {t('imports:columns.preview', { count: Math.min(PREVIEW_ROWS, data.length) })}
            </h3>
            <ScrollArea className="h-96 rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    {Array.from({ length: columnCount }, (_, i) => {
                      const field = MAPPING_FIELDS.find((f) => mapping[f] === i);
                      return (
                        <TableHead key={i}>
                          <div className="flex flex-col">
                            <span>{columnName(i)}</span>
                            {field ? (
                              <Badge variant="secondary">
                                {t(`imports:columns.fields.${field}`)}
                              </Badge>
                            ) : null}
                          </div>
                        </TableHead>
                      );
                    })}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.slice(0, PREVIEW_ROWS).map((r, ri) => (
                    <TableRow key={ri}>
                      {Array.from({ length: columnCount }, (_, ci) => (
                        <TableCell key={ci} className="font-mono text-xs">
                          {r[ci] ?? ''}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </ScrollArea>
            <div className="flex justify-between">
              <Button variant="outline" onClick={() => setStep('file')}>
                {t('common:actions.back')}
              </Button>
              <Button disabled={contractMapping === null} onClick={() => setStep('placement')}>
                {t('common:actions.continue')}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {step === 'placement' && file && contractMapping ? (
        <Card>
          <CardHeader>
            <CardTitle>{t('imports:steps.placement')}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-6">
            <Field>
              <FieldLabel>{t('imports:placement.start')}</FieldLabel>
              <RadioGroup
                value={placement.start}
                onValueChange={(v) =>
                  setPlacement((p) => ({ ...p, start: v as Placement['start'] }))
                }
              >
                {(['after_existing', 'root'] as const).map((s) => (
                  <Field key={s} orientation="horizontal">
                    <RadioGroupItem id={`placement-${s}`} value={s} />
                    <FieldLabel htmlFor={`placement-${s}`}>
                      {t(`imports:placement.starts.${s}`)}
                    </FieldLabel>
                  </Field>
                ))}
              </RadioGroup>
            </Field>
            <Field className="sm:max-w-xs">
              <FieldLabel htmlFor="placement-side">{t('imports:placement.sidePolicy')}</FieldLabel>
              <Select
                value={placement.side_policy}
                onValueChange={(v) =>
                  setPlacement((p) => ({ ...p, side_policy: v as Placement['side_policy'] }))
                }
              >
                <SelectTrigger id="placement-side" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(['alternate', 'column', 'left', 'right'] as const).map((s) => (
                    <SelectItem key={s} value={s}>
                      {t(`imports:placement.sidePolicies.${s}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {mapping.side !== undefined ? (
                <FieldDescription>{t('imports:placement.sideColumnHint')}</FieldDescription>
              ) : null}
            </Field>
            <div className="flex flex-col gap-2">
              <span className="text-sm font-medium">{t('imports:placement.piers')}</span>
              <div className="flex flex-wrap gap-1">
                {filePiers.map((p) => (
                  <Badge key={p} variant={mapPiers.includes(p) ? 'secondary' : 'destructive'}>
                    {mapPiers.includes(p) ? p : t('imports:placement.unknownPier', { code: p })}
                  </Badge>
                ))}
              </div>
              {mapPiers.length === 0 ? (
                <p className="text-muted-foreground text-sm">{t('imports:placement.noPiers')}</p>
              ) : null}
            </div>
            {importBerths.isError ? <ProblemAlert error={importBerths.error} /> : null}
            <div className="flex justify-between">
              <Button variant="outline" onClick={() => setStep('columns')}>
                {t('common:actions.back')}
              </Button>
              <Button disabled={importBerths.isPending} onClick={runDry}>
                {importBerths.isPending ? t('imports:run.running') : t('imports:run.dryRun')}
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {step === 'report' && report ? (
        <Card>
          <CardHeader>
            <CardTitle>
              {applied ? t('imports:report.appliedTitle') : t('imports:report.title')}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <ReportView report={report} />
            {importBerths.isError ? (
              <ApplyError error={importBerths.error} onReload={reload} />
            ) : null}
            {applied ? (
              <Alert>
                <CheckCircle2Icon />
                <AlertTitle>
                  {t('imports:run.applied', { count: report.planned.length })}
                </AlertTitle>
                <AlertDescription>
                  <Link
                    to="/m/$marinaId/maps/$mapId/edit"
                    params={{ marinaId, mapId: map.id }}
                    className="underline-offset-4 hover:underline"
                  >
                    {t('imports:run.openEditor')}
                  </Link>
                </AlertDescription>
              </Alert>
            ) : null}
            <div className="flex flex-wrap justify-between gap-2">
              <div className="flex gap-2">
                {applied ? null : (
                  <Button variant="outline" onClick={() => setStep('placement')}>
                    {t('common:actions.back')}
                  </Button>
                )}
                <Button
                  variant="outline"
                  onClick={() =>
                    downloadBlob(
                      `berth-import-${map.id}.csv`,
                      new Blob([reportToCsv(report)], { type: 'text/csv' }),
                    )
                  }
                >
                  <DownloadIcon />
                  {t('imports:report.download')}
                </Button>
              </div>
              {applied ? null : (
                <Button disabled={importBerths.isPending || report.valid === 0} onClick={apply}>
                  {importBerths.isPending ? t('imports:run.running') : t('imports:run.apply')}
                </Button>
              )}
            </div>
            {!applied && report.rejected.length > 0 ? (
              <p className="text-muted-foreground text-sm">
                {t('imports:run.rejectedSkipped', { count: report.rejected.length })}
              </p>
            ) : null}
          </CardContent>
        </Card>
      ) : null}
    </>
  );
}

function MappingSelect({
  field,
  value,
  columnCount,
  columnName,
  onChange,
}: {
  field: MappingField;
  value: number | undefined;
  columnCount: number;
  columnName: (i: number) => string;
  onChange: (idx: number | undefined) => void;
}) {
  const { t } = useT();
  const required = REQUIRED_FIELDS.includes(field);
  const id = `mapping-${field}`;
  return (
    <Field data-invalid={(required && value === undefined) || undefined}>
      <FieldLabel htmlFor={id}>
        {t(`imports:columns.fields.${field}`)}
        {required ? ` ${t('imports:columns.requiredMark')}` : ''}
      </FieldLabel>
      <Select
        value={value === undefined ? NONE : String(value)}
        onValueChange={(v) => onChange(v === NONE ? undefined : Number(v))}
      >
        <SelectTrigger
          id={id}
          className="w-full"
          aria-invalid={(required && value === undefined) || undefined}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>{t('imports:columns.none')}</SelectItem>
          {Array.from({ length: columnCount }, (_, i) => (
            <SelectItem key={i} value={String(i)}>
              {columnName(i)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

function ReportView({ report }: { report: Schemas['BerthImportReport'] }) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {(
          [
            ['total', report.rows_total],
            ['valid', report.valid],
            ['rejected', report.rejected.length],
            ['duplicates', report.duplicates.length],
          ] as const
        ).map(([k, v]) => (
          <div key={k} className="flex flex-col">
            <dt className="text-muted-foreground text-sm">{t(`imports:report.${k}`)}</dt>
            <dd className="text-2xl font-semibold tabular-nums" data-testid={`report-${k}`}>
              {v}
            </dd>
          </div>
        ))}
      </dl>
      {report.rejected.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-medium">{t('imports:report.rejectedTitle')}</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('imports:report.row')}</TableHead>
                <TableHead>{t('imports:report.field')}</TableHead>
                <TableHead>{t('imports:report.error')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {report.rejected.flatMap((r) =>
                r.errors.map((e, i) => (
                  <TableRow key={`${r.row}-${i}`}>
                    <TableCell className="tabular-nums">{r.row}</TableCell>
                    <TableCell>{e.field}</TableCell>
                    <TableCell className="font-mono text-xs">{e.code}</TableCell>
                  </TableRow>
                )),
              )}
            </TableBody>
          </Table>
        </section>
      ) : null}
      {report.duplicates.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-medium">{t('imports:report.duplicatesTitle')}</h3>
          <ul className="flex flex-col gap-1 text-sm">
            {report.duplicates.map((d) => (
              <li key={`${d.row}-${d.code}`}>
                {t('imports:report.duplicateLine', {
                  row: d.row,
                  code: d.code,
                  reason: t(`imports:report.reasons.${d.reason}`),
                })}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      {report.planned.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-medium">
            {t('imports:report.plannedTitle', { count: report.planned.length })}
          </h3>
          <ScrollArea className="h-72 rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('imports:report.row')}</TableHead>
                  <TableHead>{t('imports:report.code')}</TableHead>
                  <TableHead>{t('imports:report.pier')}</TableHead>
                  <TableHead>{t('imports:report.side')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {report.planned.map((p) => (
                  <TableRow key={`${p.row}-${p.code}`}>
                    <TableCell className="tabular-nums">{p.row}</TableCell>
                    <TableCell>{p.code}</TableCell>
                    <TableCell>{p.pier}</TableCell>
                    <TableCell>{t(`imports:report.sides.${p.side}`)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </ScrollArea>
        </section>
      ) : null}
    </div>
  );
}

function ApplyError({ error, onReload }: { error: unknown; onReload: () => void }) {
  const { t } = useT();
  if (isProblem(error, 'version_conflict')) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>{t('imports:run.conflict')}</AlertTitle>
        <AlertDescription>
          <Button variant="outline" size="sm" onClick={onReload}>
            {t('common:actions.reload')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  if (isProblem(error, 'map_invalid')) {
    return (
      <Alert variant="destructive">
        <TriangleAlertIcon />
        <AlertTitle>{t('imports:run.mapInvalid')}</AlertTitle>
        <AlertDescription>
          <ul className="flex list-disc flex-col gap-1 pl-5">
            {error.issues.map((issue, i) => (
              <li key={i}>
                {t('imports:run.issue', { code: issue.code, count: issue.refs.length })}
              </li>
            ))}
          </ul>
        </AlertDescription>
      </Alert>
    );
  }
  return <ProblemAlert error={error} />;
}
