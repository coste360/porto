import { getRouteApi, Link, useBlocker, useNavigate } from '@tanstack/react-router';
import { ArrowLeftIcon, LockIcon } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';

import {
  fetchPriceList,
  isProblem,
  priceListIssuesOf,
  useAllItems,
  useApi,
  useArchivePriceList,
  useBerthCategories,
  useBerthGroups,
  useDeletePriceList,
  usePriceList,
  useSavePriceList,
  validatePriceList,
  type PriceListDocument,
  type PriceListIssue,
  type PriceRule,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { ConfirmDialog, PageHeader } from '@porto/shared/ui/patterns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { toastProblem } from '../../../lib/toast';
import { localText } from '../lib/enums';
import { ActivateDialog } from './ActivateDialog';
import { BandsTab } from './BandsTab';
import { ConflictDialog } from './ConflictDialog';
import { GeneralTab } from './GeneralTab';
import {
  canonical,
  fromDocument,
  isDirty,
  issueCounts,
  issueTarget,
  refOf,
  toSaveBody,
  upsertRule,
  type EditorDoc,
} from './model';
import { PeriodsTab } from './PeriodsTab';
import { RuleSheet } from './RuleSheet';
import { RulesTab } from './RulesTab';
import { ValidationTab } from './ValidationTab';

const route = getRouteApi('/auth/m/$marinaId/price-lists/$priceListId');

type Tab = 'general' | 'periods' | 'bands' | 'rules' | 'validation';

/** Price list document editor (m2-plan §7.2): tabs, save with If-Match, activation, archive, renewal. */
export function PriceListEditorPage() {
  const { priceListId } = route.useParams();
  const { marinaId, can } = useCurrentMarina();
  const query = usePriceList(marinaId, priceListId);
  return (
    <RequirePermission allowed={can('pricing.read')}>
      {query.isPending ? (
        <LoadingState rows={6} />
      ) : query.isError ? (
        <QueryErrorState error={query.error} onRetry={() => void query.refetch()} />
      ) : (
        <Editor key={query.data.id} server={query.data} />
      )}
    </RequirePermission>
  );
}

function Editor({ server }: { server: PriceListDocument }) {
  const { t, i18n } = useT();
  const api = useApi();
  const navigate = useNavigate();
  const { marinaId, can } = useCurrentMarina();
  const items = useAllItems(marinaId);
  const categories = useBerthCategories(marinaId);
  const groups = useBerthGroups(marinaId);
  const save = useSavePriceList(marinaId, server.id);
  const archive = useArchivePriceList(marinaId);
  const remove = useDeletePriceList(marinaId);

  const [saved, setSaved] = useState<PriceListDocument>(server);
  const [doc, setDoc] = useState<EditorDoc>(() => fromDocument(server));
  const [issues, setIssues] = useState<PriceListIssue[]>(server.issues);
  const [live, setLive] = useState<{ forDoc: string; issues: PriceListIssue[] } | null>(null);
  const [checking, setChecking] = useState(false);
  const [tab, setTab] = useState<Tab>('general');
  const [openRule, setOpenRule] = useState<PriceRule | null>(null);
  const [conflict, setConflict] = useState<{ server: PriceListDocument | null } | null>(null);
  const [activating, setActivating] = useState(false);

  const savedDoc = fromDocument(saved);
  const dirty = isDirty(doc, savedDoc);
  const readOnly = saved.status !== 'draft' || !can('pricing.configure');

  // A newer version arrived (SSE invalidation, another tab): adopt it when nothing is pending locally.
  if (server.version > saved.version && !dirty) {
    setSaved(server);
    setDoc(fromDocument(server));
    setIssues(server.issues);
  }

  // Debounced dry run of the unsaved document (validatePriceList with a body).
  const docKey = canonical(doc);
  useEffect(() => {
    if (!dirty || readOnly) return;
    const ctl = new AbortController();
    const timer = setTimeout(() => {
      setChecking(true);
      validatePriceList(api, marinaId, saved.id, toSaveBody(doc), ctl.signal)
        .then((res) => setLive({ forDoc: docKey, issues: res.issues }))
        .catch(() => undefined)
        .finally(() => setChecking(false));
    }, 600);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
    // docKey covers doc; the other values are stable for the editor's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docKey, dirty, readOnly]);

  const liveIssues = dirty && live?.forDoc === docKey ? live.issues : null;
  const shown = liveIssues ?? issues;
  const counts = issueCounts(shown);

  const blocker = useBlocker({
    shouldBlockFn: () => dirty && !readOnly,
    withResolver: true,
    enableBeforeUnload: () => dirty && !readOnly,
  });

  const doSave = async (version: number) => {
    try {
      const res = await save.mutateAsync({ version, body: toSaveBody(doc) });
      setSaved(res.document);
      setDoc(fromDocument(res.document));
      setIssues(res.issues);
      setLive(null);
      setConflict(null);
      const c = issueCounts(res.issues);
      toast.success(
        c.errors > 0
          ? t('pricing:editor.savedWithErrors', { count: c.errors })
          : t('pricing:editor.saved'),
      );
    } catch (err) {
      if (isProblem(err, 'version_conflict')) {
        setConflict({ server: null });
        try {
          setConflict({ server: await fetchPriceList(api, marinaId, saved.id) });
        } catch (e2) {
          toastProblem(t, e2);
        }
      } else if (isProblem(err, 'price_list_immutable')) {
        toast.error(t('errors:price_list_immutable'));
      } else {
        const found = priceListIssuesOf(err);
        if (found.length) {
          setIssues(found);
          setTab('validation');
        }
        toastProblem(t, err);
      }
    }
  };

  const reload = (next: PriceListDocument) => {
    setSaved(next);
    setDoc(fromDocument(next));
    setIssues(next.issues);
    setLive(null);
    setConflict(null);
  };

  const doArchive = async () => {
    try {
      const res = await archive.mutateAsync({ id: saved.id, version: saved.version });
      reload(res);
      toast.success(t('pricing:lists.archived'));
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const doDelete = async () => {
    try {
      await remove.mutateAsync({ id: saved.id, version: saved.version });
      toast.success(t('pricing:lists.deleted'));
      await navigate({ to: '/m/$marinaId/price-lists', params: { marinaId } });
    } catch (err) {
      toastProblem(t, err);
    }
  };

  const describe = (issue: PriceListIssue): string => {
    const id = issue.ref.id;
    if (!id) return '';
    switch (issue.ref.kind) {
      case 'period':
        return doc.periods.find((p) => refOf(p) === id)?.code ?? '';
      case 'band':
        return doc.bands.find((b) => refOf(b) === id)?.code ?? '';
      case 'rule':
        return doc.rules.find((r) => refOf(r) === id)?.name ?? '';
      case 'item':
        return items.data?.find((i) => i.id === id)?.code ?? '';
      default:
        return '';
    }
  };

  const gotoIssue = (issue: PriceListIssue) => {
    const target = issueTarget(issue);
    setTab(target.tab);
    if (target.tab === 'rules' && target.ref && issue.ref.kind === 'rule') {
      const rule = doc.rules.find((r) => refOf(r) === target.ref);
      if (rule) setOpenRule(rule);
      return;
    }
    // Focus the row or field after the tab renders.
    const ids = [
      target.ref && target.field ? `pl-${issue.ref.kind}-${target.ref}-${target.field}` : '',
      target.ref ? `pl-${issue.ref.kind}-${target.ref}` : '',
      target.field ? `pl-field-${target.field}` : '',
    ].filter(Boolean);
    setTimeout(() => {
      for (const id of ids) {
        const el = document.getElementById(id);
        if (el) {
          el.scrollIntoView?.({ block: 'center' });
          el.focus();
          return;
        }
      }
    }, 50);
  };

  const categoryCodes = (categories.data?.items ?? [])
    .filter((c) => !c.archived)
    .map((c) => c.code);
  const groupOptions = (groups.data ?? []).map((g) => ({
    id: g.id,
    label: `${g.code} · ${localText(g.name, i18n.language)}`,
  }));
  const allItems = items.data ?? [];
  const canConfigure = can('pricing.configure');

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Button variant="ghost" size="sm" asChild>
          <Link to="/m/$marinaId/price-lists" params={{ marinaId }}>
            <ArrowLeftIcon />
            {t('pricing:lists.title')}
          </Link>
        </Button>
      </div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-2">
            {saved.code} · {localText(saved.name, i18n.language)}
            <Badge variant={saved.status === 'active' ? 'default' : 'secondary'}>
              {t(`pricingEnums:priceListStatuses.${saved.status}`)}
            </Badge>
          </span>
        }
        description={t('pricing:lists.validity', {
          from: formatIsoDate(saved.valid_from),
          to: formatIsoDate(saved.valid_to),
        })}
        actions={
          <>
            <Button variant="outline" size="sm" asChild>
              <Link to="/m/$marinaId/pricing/simulator" params={{ marinaId }}>
                {t('pricing:editor.links.simulate')}
              </Link>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <Link
                to="/m/$marinaId/price-lists/$priceListId/matrix"
                params={{ marinaId, priceListId: saved.id }}
              >
                {t('pricing:editor.links.matrix')}
              </Link>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <Link
                to="/m/$marinaId/price-lists/$priceListId/coverage"
                params={{ marinaId, priceListId: saved.id }}
              >
                {t('pricing:editor.links.coverage')}
              </Link>
            </Button>
            {canConfigure ? (
              <Button variant="outline" size="sm" asChild>
                <Link
                  to="/m/$marinaId/price-lists/$priceListId/renew"
                  params={{ marinaId, priceListId: saved.id }}
                >
                  {t('pricing:lists.renew')}
                </Link>
              </Button>
            ) : null}
            {canConfigure && saved.status === 'active' ? (
              <ConfirmDialog
                trigger={
                  <Button variant="outline" size="sm">
                    {t('pricing:lists.archive')}
                  </Button>
                }
                title={t('pricing:lists.archiveTitle', { code: saved.code })}
                description={t('pricing:lists.archiveHint')}
                confirmLabel={t('pricing:lists.archive')}
                cancelLabel={t('common:actions.cancel')}
                onConfirm={() => void doArchive()}
              />
            ) : null}
            {canConfigure && saved.status === 'draft' ? (
              <>
                <ConfirmDialog
                  trigger={
                    <Button variant="outline" size="sm">
                      {t('common:actions.delete')}
                    </Button>
                  }
                  title={t('pricing:lists.deleteTitle', { code: saved.code })}
                  description={t('pricing:lists.deleteHint')}
                  confirmLabel={t('common:actions.delete')}
                  cancelLabel={t('common:actions.cancel')}
                  destructive
                  onConfirm={() => void doDelete()}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={dirty || counts.errors > 0}
                  title={
                    dirty
                      ? t('pricing:editor.activate.saveFirst')
                      : counts.errors > 0
                        ? t('pricing:editor.activate.fixErrors')
                        : undefined
                  }
                  onClick={() => setActivating(true)}
                >
                  {t('pricing:editor.activate.open')}
                </Button>
                <Button
                  size="sm"
                  disabled={!dirty || save.isPending}
                  onClick={() => void doSave(saved.version)}
                >
                  {save.isPending ? t('common:actions.saving') : t('common:actions.save')}
                </Button>
              </>
            ) : null}
          </>
        }
      />
      {saved.status !== 'draft' ? (
        <Alert>
          <LockIcon />
          <AlertTitle>{t('pricing:editor.readOnly.title')}</AlertTitle>
          <AlertDescription>{t('pricing:editor.readOnly.description')}</AlertDescription>
        </Alert>
      ) : dirty ? (
        <p className="text-muted-foreground text-sm" role="status">
          {t('pricing:editor.unsaved')}
        </p>
      ) : null}
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList className="flex-wrap">
          <TabsTrigger value="general">{t('pricing:editor.tabs.general')}</TabsTrigger>
          <TabsTrigger value="periods">
            {t('pricing:editor.tabs.periods')} ({doc.periods.length})
          </TabsTrigger>
          <TabsTrigger value="bands">
            {t('pricing:editor.tabs.bands')} ({doc.bands.length})
          </TabsTrigger>
          <TabsTrigger value="rules">
            {t('pricing:editor.tabs.rules')} ({doc.rules.length})
          </TabsTrigger>
          <TabsTrigger value="validation">
            {t('pricing:editor.tabs.validation')}
            {counts.errors > 0 ? (
              <Badge variant="destructive">{counts.errors}</Badge>
            ) : counts.warnings > 0 ? (
              <Badge variant="outline">{counts.warnings}</Badge>
            ) : null}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="general" className="pt-4">
          <GeneralTab doc={doc} onChange={setDoc} readOnly={readOnly} items={allItems} />
        </TabsContent>
        <TabsContent value="periods" className="pt-4">
          <PeriodsTab doc={doc} onChange={setDoc} readOnly={readOnly} />
        </TabsContent>
        <TabsContent value="bands" className="pt-4">
          <BandsTab doc={doc} onChange={setDoc} readOnly={readOnly} categories={categoryCodes} />
        </TabsContent>
        <TabsContent value="rules" className="pt-4">
          <RulesTab
            doc={doc}
            onChange={setDoc}
            readOnly={readOnly}
            items={allItems}
            issues={shown}
            onOpen={setOpenRule}
          />
        </TabsContent>
        <TabsContent value="validation" className="pt-4">
          <ValidationTab
            issues={shown}
            live={liveIssues !== null}
            pending={checking}
            describe={describe}
            onGoto={gotoIssue}
          />
        </TabsContent>
      </Tabs>
      <RuleSheet
        rule={openRule}
        readOnly={readOnly}
        items={allItems}
        bands={doc.bands}
        categories={categoryCodes}
        periods={doc.periods.map((p) => p.code)}
        groups={groupOptions}
        onClose={() => setOpenRule(null)}
        onSubmit={(rule) => {
          setDoc(upsertRule(doc, rule));
          setOpenRule(null);
        }}
      />
      <ConflictDialog
        open={conflict !== null}
        local={doc}
        server={conflict?.server ?? null}
        onCancel={() => setConflict(null)}
        onReload={() => {
          if (conflict?.server) reload(conflict.server);
          else setConflict(null);
        }}
        onOverwrite={() => {
          if (conflict?.server) void doSave(conflict.server.version);
        }}
      />
      <ActivateDialog
        marinaId={marinaId}
        doc={saved}
        open={activating}
        onClose={() => setActivating(false)}
        onInvalid={(found) => {
          setIssues(found);
          setTab('validation');
        }}
      />
      <AlertDialog open={blocker.status === 'blocked'}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('pricing:editor.leave.title')}</AlertDialogTitle>
            <AlertDialogDescription>{t('pricing:editor.leave.description')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => blocker.reset?.()}>
              {t('pricing:editor.leave.stay')}
            </AlertDialogCancel>
            <AlertDialogAction onClick={() => blocker.proceed?.()}>
              {t('pricing:editor.leave.leave')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
