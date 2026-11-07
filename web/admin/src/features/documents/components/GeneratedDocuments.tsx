import { DownloadIcon, RotateCwIcon } from 'lucide-react';

import {
  useGeneratedDocumentLink,
  useGeneratedDocuments,
  useRetryRender,
  type DocumentSubjectType,
  type GeneratedDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState } from '../../../components/PageStates';
import { M4ProblemAlert } from '../../contracts-m4/components/kit';

function documentVariant(d: Pick<GeneratedDocument, 'status' | 'signed'>) {
  if (d.status === 'failed') return 'destructive' as const;
  if (d.signed) return 'default' as const;
  return d.status === 'ready' ? ('secondary' as const) : ('outline' as const);
}

/**
 * Documents generated for one subject (contract, addendum, quote, transfer…): kind, locale, status, signed flag
 * and a 5-minute download link (customers.read). Failed renders can be retried (contracts.write).
 */
export function GeneratedDocuments({
  subjectType,
  subjectId,
}: {
  subjectType: DocumentSubjectType;
  subjectId: string;
}) {
  const { t, i18n } = useT();
  const { marinaId, can } = useCurrentMarina();
  const docs = useGeneratedDocuments(marinaId, subjectType, subjectId);
  const link = useGeneratedDocumentLink(marinaId);
  const retry = useRetryRender(marinaId);
  if (docs.isPending) return <LoadingState rows={2} />;
  if (docs.isError)
    return <QueryErrorState error={docs.error} onRetry={() => void docs.refetch()} />;
  return (
    <div className="flex flex-col gap-3">
      <M4ProblemAlert error={link.error ?? retry.error} />
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('templates:docs.kind')}</TableHead>
              <TableHead>{t('templates:docs.locale')}</TableHead>
              <TableHead>{t('templates:docs.status')}</TableHead>
              <TableHead>{t('templates:docs.renderedAt')}</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {docs.data.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground text-center">
                  {t('templates:docs.empty')}
                </TableCell>
              </TableRow>
            ) : (
              docs.data.map((d) => (
                <TableRow key={d.id}>
                  <TableCell>{t(`docsEnums:templateKind.${d.kind}`)}</TableCell>
                  <TableCell>{d.locale.toUpperCase()}</TableCell>
                  <TableCell className="flex flex-wrap gap-1">
                    <Badge variant={documentVariant(d)}>
                      {t(`docsEnums:documentStatus.${d.status}`)}
                    </Badge>
                    {d.signed ? <Badge>{t('templates:docs.signed')}</Badge> : null}
                    {d.pdfa ? <Badge variant="outline">{t('templates:docs.pdfa')}</Badge> : null}
                  </TableCell>
                  <TableCell>
                    {d.rendered_at ? formatDateTime(d.rendered_at, i18n.language) : null}
                  </TableCell>
                  <TableCell className="text-right">
                    {d.status === 'ready' && can('customers.read') ? (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={link.isPending}
                        onClick={() =>
                          link.mutate(d.id, {
                            onSuccess: (l) => window.open(l.url, '_blank', 'noopener'),
                          })
                        }
                      >
                        <DownloadIcon />
                        {t('templates:docs.download')}
                      </Button>
                    ) : null}
                    {d.status === 'failed' && can('contracts.write') ? (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={retry.isPending}
                        onClick={() => retry.mutate(d.id)}
                      >
                        <RotateCwIcon />
                        {t('templates:docs.retry')}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
