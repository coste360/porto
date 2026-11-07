import { Link, useNavigate } from '@tanstack/react-router';
import { PlusIcon } from 'lucide-react';
import { useState } from 'react';

import {
  useCreateTemplate,
  useTemplateKinds,
  useTemplates,
  type TemplateKind,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Field, FieldLabel } from '@porto/shared/ui/field';
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
import { EnumSelect, M4ProblemAlert, PageFrame, Section } from '../contracts-m4/components/kit';

/** Print templates (spec 15.1–15.3): kinds of the catalogue, marina templates and their locales. */
export function TemplatesPage() {
  const { t } = useT();
  const { marinaId, can } = useCurrentMarina();
  const navigate = useNavigate();
  const kinds = useTemplateKinds(marinaId);
  const [kind, setKind] = useState<TemplateKind | ''>('');
  const templates = useTemplates(marinaId, kind || undefined);
  const create = useCreateTemplate(marinaId);
  const [newKind, setNewKind] = useState<TemplateKind | ''>('');
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const publishable = (kinds.data ?? []).filter((k) => k.publishable).map((k) => k.kind);
  return (
    <RequirePermission allowed={can('templates.configure')}>
      <PageFrame>
        <PageHeader
          title={t('templates:list.title')}
          description={t('templates:list.description')}
        />
        <Section title={t('templates:list.new')} description={t('templates:list.newHint')}>
          <div className="flex flex-wrap items-end gap-2">
            <EnumSelect
              id="tpl-new-kind"
              label={t('templates:list.kind')}
              group="templateKind"
              values={publishable}
              value={newKind}
              onChange={setNewKind}
            />
            <Field className="w-40">
              <FieldLabel htmlFor="tpl-code">{t('templates:list.code')}</FieldLabel>
              <Input
                id="tpl-code"
                value={code}
                maxLength={40}
                onChange={(e) => setCode(e.target.value)}
              />
            </Field>
            <Field className="w-64">
              <FieldLabel htmlFor="tpl-name">{t('templates:list.name')}</FieldLabel>
              <Input
                id="tpl-name"
                value={name}
                maxLength={120}
                onChange={(e) => setName(e.target.value)}
              />
            </Field>
            <Button
              disabled={!newKind || !code.trim() || !name.trim() || create.isPending}
              onClick={() =>
                newKind &&
                create.mutate(
                  { kind: newKind, code: code.trim(), name: name.trim(), from_builtin: true },
                  {
                    onSuccess: (tp) =>
                      void navigate({
                        to: '/m/$marinaId/templates/$templateId',
                        params: { marinaId, templateId: tp.id },
                      }),
                  },
                )
              }
            >
              <PlusIcon />
              {t('templates:list.create')}
            </Button>
          </div>
          <M4ProblemAlert error={create.error} />
        </Section>
        <EnumSelect
          id="tpl-kind"
          label={t('templates:list.kind')}
          group="templateKind"
          values={(kinds.data ?? []).map((k) => k.kind)}
          value={kind}
          onChange={setKind}
          allLabel={t('contractsM4:common.all')}
        />
        {templates.isPending ? (
          <LoadingState rows={5} />
        ) : templates.isError ? (
          <QueryErrorState error={templates.error} onRetry={() => void templates.refetch()} />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('templates:list.name')}</TableHead>
                  <TableHead>{t('templates:list.kind')}</TableHead>
                  <TableHead>{t('templates:list.locales')}</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {templates.data.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground h-24 text-center">
                      {t('templates:list.empty')}
                    </TableCell>
                  </TableRow>
                ) : (
                  templates.data.map((tp) => (
                    <TableRow key={tp.id}>
                      <TableCell>
                        <Link
                          to="/m/$marinaId/templates/$templateId"
                          params={{ marinaId, templateId: tp.id }}
                          className="font-medium underline-offset-4 hover:underline"
                        >
                          {tp.name}
                        </Link>
                        <span className="text-muted-foreground ml-2 text-xs">{tp.code}</span>
                      </TableCell>
                      <TableCell>{t(`docsEnums:templateKind.${tp.kind}`)}</TableCell>
                      <TableCell className="flex flex-wrap gap-1">
                        {tp.locales.map((l) => (
                          <Badge key={l.locale} variant="outline">
                            {t('templates:list.localeVersion', {
                              locale: l.locale.toUpperCase(),
                              version: l.version,
                            })}
                          </Badge>
                        ))}
                      </TableCell>
                      <TableCell className="flex gap-1">
                        {tp.is_default ? <Badge>{t('templates:list.default')}</Badge> : null}
                        {tp.archived_at ? (
                          <Badge variant="secondary">{t('templates:list.archived')}</Badge>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        )}
      </PageFrame>
    </RequirePermission>
  );
}
