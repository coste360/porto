import type { DaySubject, LensStyle } from '@porto/shared/map';
import { TOKEN_CLASSES } from '@porto/shared/map';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

export type ListGroup = { id: string; label: string; items: DaySubject[] };

/** List alternative of the map, by structure (spec 9.1): every element is a button that opens its quick card. */
export function ElementList({
  groups,
  styles,
  dimmed,
  onPick,
}: {
  groups: readonly ListGroup[];
  styles: ReadonlyMap<string, LensStyle>;
  dimmed: ReadonlySet<string>;
  onPick: (ref: string) => void;
}) {
  const { t } = useT();
  return (
    <div className="flex flex-col gap-4" data-testid="ops-list">
      {groups.map((g) => (
        <section key={g.id} aria-label={g.label}>
          <h3 className="mb-1 text-sm font-semibold">{g.label}</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('opsMap:list.code')}</TableHead>
                <TableHead>{t('opsMap:list.status')}</TableHead>
                <TableHead>{t('opsMap:list.vessel')}</TableHead>
                <TableHead>{t('opsMap:list.legend')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {g.items.map((s) => {
                const st = styles.get(s.ref);
                return (
                  <TableRow
                    key={s.ref}
                    data-dimmed={dimmed.has(s.ref) || undefined}
                    className={dimmed.has(s.ref) ? 'opacity-50' : undefined}
                  >
                    <TableCell>
                      <Button variant="link" className="h-auto p-0" onClick={() => onPick(s.ref)}>
                        {s.code}
                      </Button>
                    </TableCell>
                    <TableCell>{t(`occupancyEnums:dayStatus.${s.subject.status}`)}</TableCell>
                    <TableCell>{s.subject.vessel?.name ?? ''}</TableCell>
                    <TableCell>
                      {st ? (
                        <span className="inline-flex items-center gap-2">
                          <span
                            className={`size-3 rounded-sm ${TOKEN_CLASSES[st.colorToken].bg}`}
                            aria-hidden
                          />
                          {t(`occupancyEnums:lens.legend.${st.legendKey}`)}
                        </span>
                      ) : null}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </section>
      ))}
    </div>
  );
}
