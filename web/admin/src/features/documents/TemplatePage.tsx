import { getRouteApi } from '@tanstack/react-router';
import { ChevronRightIcon, EyeIcon, SaveIcon, SendIcon } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';

import {
  usePreviewTemplate,
  usePublishTemplateVersion,
  useSaveTemplateVersion,
  useTemplate,
  useTemplateKinds,
  useTemplateVersions,
  type DocumentSubjectType,
  type Schemas,
  type Template,
  type TemplateVersion,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@porto/shared/ui/collapsible';
import { Field, FieldLabel } from '@porto/shared/ui/field';
import { Input } from '@porto/shared/ui/input';
import { PageHeader } from '@porto/shared/ui/patterns';
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@porto/shared/ui/resizable';
import { Tabs, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';
import { Textarea } from '@porto/shared/ui/textarea';

import { useCurrentMarina } from '../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../components/PageStates';
import { EnumSelect, M4ProblemAlert, PageFrame } from '../contracts-m4/components/kit';
import {
  PreviewUrl,
  eachSnippet,
  enclosingEach,
  fieldTree,
  ifSnippet,
  insertAt,
  offsetOf,
  templateIssues,
  tokenFor,
  type FieldNode,
  type TemplateIssue,
} from './lib/templateEditor';

const route = getRouteApi('/auth/m/$marinaId/templates/$templateId');
const LOCALES: readonly Schemas['Locale'][] = ['it', 'en'];
const PARTS = ['body', 'header', 'footer'] as const;
type Part = (typeof PARTS)[number];
const SUBJECTS: readonly DocumentSubjectType[] = [
  'quote',
  'contract',
  'addendum',
  'renewal_proposal',
  'right_transfer',
  'communication',
  'letter',
];

/** Templates editor (spec 15.2, m4-plan §8.1): one template, its versions per locale, editor and PDF preview. */
export function TemplatePage() {
  const { marinaId, can } = useCurrentMarina();
  const { templateId } = route.useParams();
  const template = useTemplate(marinaId, templateId);
  return (
    <RequirePermission allowed={can('templates.configure')}>
      <PageFrame>
        {template.isPending ? (
          <LoadingState rows={8} />
        ) : template.isError ? (
          <QueryErrorState error={template.error} onRetry={() => void template.refetch()} />
        ) : (
          <TemplateWorkspace template={template.data} />
        )}
      </PageFrame>
    </RequirePermission>
  );
}

function TemplateWorkspace({ template }: { template: Template }) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [locale, setLocale] = useState<Schemas['Locale']>(template.locales[0]?.locale ?? 'it');
  const versions = useTemplateVersions(marinaId, template.id, locale);
  const kinds = useTemplateKinds(marinaId);
  const fields = kinds.data?.find((k) => k.kind === template.kind)?.fields ?? [];
  const latest = versions.data?.slice().sort((a, b) => b.version - a.version)[0];
  return (
    <>
      <PageHeader
        title={template.name}
        description={t(`docsEnums:templateKind.${template.kind}`)}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Tabs value={locale} onValueChange={(v) => setLocale(v as Schemas['Locale'])}>
          <TabsList aria-label={t('templates:editor.locale')}>
            {LOCALES.map((l) => (
              <TabsTrigger key={l} value={l}>
                {l.toUpperCase()}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        {(versions.data ?? [])
          .slice()
          .sort((a, b) => b.version - a.version)
          .slice(0, 5)
          .map((v) => (
            <Badge key={v.id} variant={v.status === 'published' ? 'default' : 'outline'}>
              {t('templates:editor.versionBadge', {
                version: v.version,
                status: t(`docsEnums:templateVersionStatus.${v.status}`),
              })}
            </Badge>
          ))}
      </div>
      {versions.isPending || kinds.isPending ? (
        <LoadingState rows={8} />
      ) : versions.isError ? (
        <QueryErrorState error={versions.error} onRetry={() => void versions.refetch()} />
      ) : (
        // Keyed by locale only: a version saved from the editor becomes the latest, and remounting on it would
        // drop the editor state (preview, draft to publish) and the save confirmation.
        <TemplateEditor
          key={locale}
          template={template}
          locale={locale}
          fields={fields}
          latest={latest}
        />
      )}
    </>
  );
}

export function TemplateEditor({
  template,
  locale,
  fields,
  latest,
}: {
  template: Template;
  locale: Schemas['Locale'];
  fields: Schemas['FieldDef'][];
  latest: TemplateVersion | undefined;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const save = useSaveTemplateVersion(marinaId);
  const publish = usePublishTemplateVersion(marinaId);
  const preview = usePreviewTemplate(marinaId);
  const [part, setPart] = useState<Part>('body');
  const [text, setText] = useState<Record<Part, string>>({
    body: latest?.body_md ?? '',
    header: latest?.header_md ?? '',
    footer: latest?.footer_md ?? '',
  });
  const [draft, setDraft] = useState<TemplateVersion | undefined>(
    latest?.status === 'draft' ? latest : undefined,
  );
  const [issues, setIssues] = useState<TemplateIssue[]>([]);
  const [sampleKind, setSampleKind] = useState<DocumentSubjectType | ''>('');
  const [sampleId, setSampleId] = useState('');
  const [pdfUrl, setPdfUrl] = useState<string | null>(null);
  const urls = useRef(new PreviewUrl());
  const area = useRef<HTMLTextAreaElement>(null);
  // Last selection inside the textarea; null until the user places the caret (insertions then go at the end).
  const caretRef = useRef<{ start: number; end: number } | null>(null);
  const rememberCaret = () => {
    const el = area.current;
    if (el) caretRef.current = { start: el.selectionStart, end: el.selectionEnd };
  };
  const tree = useMemo(() => fieldTree(fields), [fields]);
  useEffect(() => {
    const holder = urls.current;
    return () => holder.clear();
  }, []);

  const insert = (snippet: string) => {
    const el = area.current;
    const value = text[part];
    const start = caretRef.current?.start ?? value.length;
    const end = caretRef.current?.end ?? value.length;
    const r = insertAt(value, start, end, snippet);
    setText({ ...text, [part]: r.value });
    caretRef.current = { start: r.caret, end: r.caret };
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(r.caret, r.caret);
    });
  };
  const insertField = (f: Schemas['FieldDef']) => {
    const caret = caretRef.current?.start ?? text[part].length;
    insert(tokenFor(f, enclosingEach(text[part], caret)));
  };
  const goTo = (i: TemplateIssue) => {
    setPart(i.part);
    requestAnimationFrame(() => {
      const el = area.current;
      if (!el) return;
      const off = offsetOf(text[i.part], i.line, i.col);
      el.focus();
      el.setSelectionRange(off, off);
      caretRef.current = { start: off, end: off };
    });
  };
  const body = (): Schemas['TemplateVersionInput'] => ({
    locale,
    body_md: text.body,
    ...(text.header ? { header_md: text.header } : {}),
    ...(text.footer ? { footer_md: text.footer } : {}),
  });
  const onError = (e: unknown) => setIssues(templateIssues(e));
  const sample: Schemas['TemplatePreviewSample'] =
    sampleKind && sampleId.trim()
      ? { subject_type: sampleKind, subject_id: sampleId.trim() }
      : 'demo';
  const lineIssues = issues.filter((i) => i.part === part && i.line > 0);
  const lines = text[part].split('\n');

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          disabled={save.isPending}
          onClick={() =>
            save.mutate(
              { templateId: template.id, body: body() },
              {
                onSuccess: (v) => {
                  setIssues([]);
                  setDraft(v);
                  toast.success(t('templates:editor.saved', { version: v.version }));
                },
                onError,
              },
            )
          }
        >
          <SaveIcon />
          {t('templates:editor.save')}
        </Button>
        <Button
          disabled={!draft || publish.isPending}
          onClick={() =>
            draft &&
            publish.mutate(
              { templateId: template.id, version: draft.version, locale },
              {
                onSuccess: (v) => {
                  setDraft(undefined);
                  toast.success(t('templates:editor.published', { version: v.version }));
                },
                onError,
              },
            )
          }
        >
          <SendIcon />
          {t('templates:editor.publish')}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => insert(eachSnippet('list'))}>
          {t('templates:editor.each')}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => insert(ifSnippet('field'))}>
          {t('templates:editor.if')}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => insert('{{pagebreak}}')}>
          {t('templates:editor.pagebreak')}
        </Button>
        <Button variant="ghost" size="sm" onClick={() => insert('{{signature "customer"}}')}>
          {t('templates:editor.signature')}
        </Button>
      </div>
      {issues.length > 0 ? (
        <ul
          className="text-destructive flex flex-col gap-1 text-sm"
          role="alert"
          data-testid="template-issues"
        >
          {issues.map((i, n) => (
            <li key={n}>
              <Button
                variant="link"
                size="sm"
                className="text-destructive h-auto p-0"
                onClick={() => goTo(i)}
              >
                {i.line > 0
                  ? t('templates:editor.issueAt', {
                      part: t(`templates:editor.parts.${i.part}`),
                      line: i.line,
                      col: i.col,
                    })
                  : t('templates:editor.issue')}
              </Button>{' '}
              {t(`templates:errors.${i.code}`, { defaultValue: t('templates:errors.invalid') })}
              {i.path ? ` (${i.path})` : ''}
            </li>
          ))}
        </ul>
      ) : null}
      <M4ProblemAlert
        error={issues.length === 0 ? (save.error ?? publish.error ?? preview.error) : null}
      />
      <ResizablePanelGroup orientation="horizontal" className="min-h-96 rounded-md border">
        <ResizablePanel defaultSize="22%" minSize="12%">
          <nav
            aria-label={t('templates:editor.fields')}
            className="flex max-h-screen flex-col gap-1 overflow-auto p-2 text-sm"
          >
            <h2 className="text-muted-foreground text-xs font-medium uppercase">
              {t('templates:editor.fields')}
            </h2>
            <FieldTree nodes={tree} onInsert={insertField} />
          </nav>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize="40%" minSize="25%">
          <div className="flex h-full flex-col gap-2 p-2">
            <Tabs
              value={part}
              onValueChange={(v) => {
                caretRef.current = null;
                setPart(v as Part);
              }}
            >
              <TabsList>
                {PARTS.map((p) => (
                  <TabsTrigger key={p} value={p}>
                    {t(`templates:editor.parts.${p}`)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            <Textarea
              ref={area}
              aria-label={t('templates:editor.text', { part: t(`templates:editor.parts.${part}`) })}
              aria-invalid={lineIssues.length > 0 || undefined}
              className="min-h-80 flex-1 font-mono text-sm"
              value={text[part]}
              onChange={(e) => {
                setText({ ...text, [part]: e.target.value });
                rememberCaret();
              }}
              onSelect={rememberCaret}
              onKeyUp={rememberCaret}
              onClick={rememberCaret}
            />
            {lineIssues.length > 0 ? (
              <ol
                className="flex flex-col gap-0.5 font-mono text-xs"
                aria-label={t('templates:editor.markers')}
              >
                {lineIssues.map((i, n) => (
                  <li key={n} className="text-destructive" data-line={i.line}>
                    {`${i.line}: ${lines[i.line - 1] ?? ''}`}
                  </li>
                ))}
              </ol>
            ) : null}
          </div>
        </ResizablePanel>
        <ResizableHandle withHandle />
        <ResizablePanel defaultSize="38%" minSize="20%">
          <div className="flex h-full flex-col gap-2 p-2">
            <div className="flex flex-wrap items-end gap-2">
              <EnumSelect
                id="tpl-sample-kind"
                label={t('templates:editor.sample')}
                group="templateKind"
                values={SUBJECTS}
                labelOf={(s) => t(`templates:editor.subjects.${s}`)}
                value={sampleKind}
                onChange={setSampleKind}
                allLabel={t('templates:editor.demo')}
              />
              {sampleKind ? (
                <Field className="w-56">
                  <FieldLabel htmlFor="tpl-sample-id">{t('templates:editor.sampleId')}</FieldLabel>
                  <Input
                    id="tpl-sample-id"
                    value={sampleId}
                    onChange={(e) => setSampleId(e.target.value)}
                  />
                </Field>
              ) : null}
              <Button
                variant="outline"
                disabled={preview.isPending}
                onClick={() =>
                  preview.mutate(
                    {
                      templateId: template.id,
                      body: {
                        locale,
                        body_md: text.body,
                        ...(text.header ? { header_md: text.header } : {}),
                        ...(text.footer ? { footer_md: text.footer } : {}),
                        sample,
                      },
                    },
                    {
                      onSuccess: (blob) => {
                        setIssues([]);
                        setPdfUrl(urls.current.set(blob));
                      },
                      onError,
                    },
                  )
                }
              >
                <EyeIcon />
                {t('templates:editor.preview')}
              </Button>
            </div>
            {pdfUrl ? (
              <iframe
                title={t('templates:editor.previewTitle')}
                src={pdfUrl}
                className="min-h-96 w-full flex-1 rounded-md border"
                data-testid="template-preview"
              />
            ) : (
              <p className="text-muted-foreground text-sm">{t('templates:editor.previewHint')}</p>
            )}
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

/** Description keys of the catalogue are relative to docsEnums (`fields.customer`). */
function descriptionKey(key: string): string {
  return key.includes(':') ? key : `docsEnums:${key}`;
}

function FieldTree({
  nodes,
  onInsert,
}: {
  nodes: FieldNode[];
  onInsert: (f: Schemas['FieldDef']) => void;
}) {
  const { t } = useT();
  return (
    <ul className="flex flex-col gap-0.5">
      {nodes.map((n) =>
        n.children.length > 0 ? (
          <li key={n.path}>
            <Collapsible>
              <div className="flex items-center gap-1">
                <CollapsibleTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-6"
                    aria-label={t('templates:editor.expand', { path: n.path })}
                  >
                    <ChevronRightIcon />
                  </Button>
                </CollapsibleTrigger>
                {n.field ? (
                  <Button
                    variant="link"
                    size="sm"
                    className="h-auto p-0"
                    onClick={() => onInsert(n.field!)}
                  >
                    {n.name}
                  </Button>
                ) : (
                  <span>{n.name}</span>
                )}
              </div>
              <CollapsibleContent className="ml-4">
                <FieldTree nodes={n.children} onInsert={onInsert} />
              </CollapsibleContent>
            </Collapsible>
          </li>
        ) : (
          <li key={n.path}>
            <Button
              variant="link"
              size="sm"
              className="h-auto p-0"
              title={
                n.field
                  ? t(descriptionKey(n.field.description_key), { defaultValue: n.path })
                  : n.path
              }
              onClick={() => n.field && onInsert(n.field)}
            >
              {n.name}
            </Button>
          </li>
        ),
      )}
    </ul>
  );
}
