import { useState } from 'react';
import { toast } from 'sonner';

import {
  useOnlineQuotaUsage,
  useOnlineQuotas,
  useUpdateOnlineQuotas,
  type OnlineQuota,
  type OnlineQuotaKind,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@porto/shared/ui/card';
import { Input } from '@porto/shared/ui/input';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { ScrollArea, ScrollBar } from '@porto/shared/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@porto/shared/ui/select';
import { Skeleton } from '@porto/shared/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

type Draft = Record<string, { kind: OnlineQuotaKind; value: number }>;

function isoDay(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/** Online quotas per berth group and the usage of the next 30 nights. */
export function QuotasTab({ marinaId }: { marinaId: string }) {
  const { t, i18n } = useT();
  const quotas = useOnlineQuotas(marinaId);
  const update = useUpdateOnlineQuotas(marinaId);
  const [draft, setDraft] = useState<Draft>({});
  const [from] = useState(() => isoDay(new Date()));
  const [to] = useState(() => isoDay(new Date(Date.now() + 29 * 86_400_000)));
  const usage = useOnlineQuotaUsage(marinaId, from, to);

  if (quotas.isPending) return <Skeleton className="h-48 w-full" />;
  if (quotas.isError) return <ProblemAlert error={quotas.error} />;
  const value = (q: OnlineQuota) => draft[q.group_id] ?? { kind: q.kind, value: q.value };
  const set = (q: OnlineQuota, patch: Partial<{ kind: OnlineQuotaKind; value: number }>) =>
    setDraft((d) => ({ ...d, [q.group_id]: { ...value(q), ...patch } }));
  const save = () =>
    update.mutate(
      {
        items: Object.entries(draft).map(([group_id, v]) => ({
          group_id,
          kind: v.kind,
          value: v.value,
        })),
      },
      {
        onSuccess: () => {
          setDraft({});
          toast.success(t('online:settings.quotas.saved'));
        },
      },
    );
  const name = (q: OnlineQuota) =>
    i18n.language.startsWith('en') && q.group_name.en ? q.group_name.en : q.group_name.it;
  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>{t('online:settings.tabs.quotas')}</CardTitle>
          <CardDescription>{t('online:settings.quotas.description')}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {quotas.data.items.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('online:settings.quotas.empty')}</p>
          ) : (
            <Table data-testid="quotas-table">
              <TableHeader>
                <TableRow>
                  <TableHead>{t('online:settings.quotas.group')}</TableHead>
                  <TableHead>{t('online:settings.quotas.members')}</TableHead>
                  <TableHead>{t('online:settings.quotas.kind')}</TableHead>
                  <TableHead>{t('online:settings.quotas.value')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {quotas.data.items.map((q) => {
                  const v = value(q);
                  return (
                    <TableRow key={q.group_id}>
                      <TableCell>
                        <span className="font-medium">{q.group_code}</span> {name(q)}
                        {q.has_areas ? (
                          <p className="text-muted-foreground text-xs">
                            {t('online:settings.quotas.areasOnlyCount')}
                          </p>
                        ) : null}
                      </TableCell>
                      <TableCell className="tabular-nums">{q.member_berths}</TableCell>
                      <TableCell>
                        <Select
                          value={v.kind}
                          onValueChange={(k) => set(q, { kind: k as OnlineQuotaKind })}
                        >
                          <SelectTrigger
                            aria-label={t('online:settings.quotas.kind')}
                            className="w-40"
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="none">{t('online:settings.quotas.none')}</SelectItem>
                            <SelectItem value="count">
                              {t('online:settings.quotas.count')}
                            </SelectItem>
                            <SelectItem value="percent" disabled={q.has_areas}>
                              {t('online:settings.quotas.percent')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <Input
                          type="number"
                          inputMode="numeric"
                          min={0}
                          max={v.kind === 'percent' ? 100 : undefined}
                          className="w-24"
                          disabled={v.kind === 'none'}
                          aria-label={t('online:settings.quotas.value')}
                          value={v.value}
                          onChange={(e) =>
                            set(q, { value: Math.max(0, Math.trunc(Number(e.target.value) || 0)) })
                          }
                        />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
          <ProblemAlert error={update.error} />
          <div>
            <Button
              onClick={save}
              disabled={update.isPending || Object.keys(draft).length === 0}
              data-testid="quotas-save"
            >
              {t('online:settings.quotas.save')}
            </Button>
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>{t('online:settings.quotas.usage')}</CardTitle>
        </CardHeader>
        <CardContent>
          {usage.isPending ? (
            <Skeleton className="h-24 w-full" />
          ) : usage.isError ? (
            <ProblemAlert error={usage.error} />
          ) : usage.data.groups.length === 0 ? (
            <p className="text-muted-foreground text-sm">{t('online:settings.quotas.empty')}</p>
          ) : (
            <ScrollArea className="w-full">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('online:settings.quotas.group')}</TableHead>
                    {usage.data.groups[0]?.nights.map((n) => (
                      <TableHead key={n.night} className="text-xs">
                        {formatIsoDate(n.night).slice(0, 5)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {usage.data.groups.map((g) => {
                    const q = quotas.data.items.find((x) => x.group_id === g.group_id);
                    return (
                      <TableRow key={g.group_id}>
                        <TableCell className="font-medium">{q?.group_code ?? ''}</TableCell>
                        {g.nights.map((n) => (
                          <TableCell
                            key={n.night}
                            className={
                              n.cap > 0 && n.used >= n.cap
                                ? 'bg-destructive/10 text-xs tabular-nums'
                                : n.used > 0
                                  ? 'bg-primary/10 text-xs tabular-nums'
                                  : 'text-xs tabular-nums'
                            }
                          >
                            {`${n.used}/${n.cap}`}
                          </TableCell>
                        ))}
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
              <ScrollBar orientation="horizontal" />
            </ScrollArea>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
