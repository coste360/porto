import { ArrowDownIcon, ArrowUpIcon, PlusIcon, Trash2Icon } from 'lucide-react';

import type { PriceBand } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Input } from '@porto/shared/ui/input';
import { LengthInput } from '@porto/shared/ui/patterns';
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

import { rangeLabel } from '../lib/labels';
import { bandRanges, newKey, refOf, renameBand, type EditorDoc } from './model';

const NONE = '__none__';

export type BandsTabProps = {
  doc: EditorDoc;
  onChange: (doc: EditorDoc) => void;
  readOnly: boolean;
  categories: string[];
};

/** Tab Fasce: ordered length bands, inclusive upper bound in metres, optional berth category. */
export function BandsTab({ doc, onChange, readOnly, categories }: BandsTabProps) {
  const { t, i18n } = useT();
  const ranges = bandRanges(doc.bands);
  const set = (i: number, b: PriceBand) =>
    onChange({ ...doc, bands: doc.bands.map((x, j) => (j === i ? b : x)) });
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= doc.bands.length) return;
    const bands = [...doc.bands];
    [bands[i], bands[j]] = [bands[j]!, bands[i]!];
    onChange({ ...doc, bands });
  };
  const add = () => {
    const last = doc.bands.at(-1)?.max_cm;
    onChange({
      ...doc,
      bands: [
        ...doc.bands,
        {
          key: newKey('b'),
          code: `F${doc.bands.length + 1}`,
          ...(last !== undefined ? { max_cm: last + 200 } : { max_cm: 800 }),
        },
      ],
    });
  };
  const m = t('pricing:units.m');

  return (
    <div className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">{t('pricing:bands.hint')}</p>
      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('pricing:bands.fields.code')}</TableHead>
              <TableHead>{t('pricing:bands.fields.max')}</TableHead>
              <TableHead>{t('pricing:bands.fields.range')}</TableHead>
              <TableHead>{t('pricing:bands.fields.category')}</TableHead>
              <TableHead>
                <span className="sr-only">{t('common:actions.actions')}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {doc.bands.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground h-16 text-center">
                  {t('pricing:bands.empty')}
                </TableCell>
              </TableRow>
            ) : (
              doc.bands.map((b, i) => {
                const id = `pl-band-${refOf(b) || i}`;
                return (
                  <TableRow key={refOf(b) || i} id={id} tabIndex={-1}>
                    <TableCell className="min-w-28">
                      <Input
                        id={`${id}-code`}
                        aria-label={t('pricing:bands.codeOf', { n: i + 1 })}
                        value={b.code}
                        maxLength={32}
                        disabled={readOnly}
                        autoComplete="off"
                        onChange={(e) => onChange(renameBand(doc, i, e.target.value))}
                      />
                    </TableCell>
                    <TableCell className="min-w-36">
                      <LengthInput
                        id={`${id}-max`}
                        aria-label={t('pricing:bands.maxOf', { n: i + 1 })}
                        value={b.max_cm ?? null}
                        unit={m}
                        locale={i18n.language}
                        disabled={readOnly}
                        placeholder={t('pricing:bands.open')}
                        onChange={(cm) => {
                          const { max_cm: _old, ...rest } = b;
                          set(i, cm === null || cm <= 0 ? rest : { ...rest, max_cm: cm });
                        }}
                      />
                    </TableCell>
                    <TableCell
                      className="text-sm whitespace-nowrap"
                      data-testid={`band-range-${i}`}
                    >
                      {rangeLabel(t, ranges[i]!, i18n.language)}
                    </TableCell>
                    <TableCell className="min-w-36">
                      <Select
                        value={b.category || NONE}
                        disabled={readOnly}
                        onValueChange={(v) => {
                          const { category: _c, ...rest } = b;
                          set(i, v === NONE ? rest : { ...rest, category: v });
                        }}
                      >
                        <SelectTrigger
                          className="w-full"
                          aria-label={t('pricing:bands.categoryOf', { n: i + 1 })}
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value={NONE}>{t('pricing:bands.noCategory')}</SelectItem>
                          {[...new Set([...categories, ...(b.category ? [b.category] : [])])].map(
                            (c) => (
                              <SelectItem key={c} value={c}>
                                {c}
                              </SelectItem>
                            ),
                          )}
                        </SelectContent>
                      </Select>
                    </TableCell>
                    <TableCell>
                      {readOnly ? null : (
                        <div className="flex justify-end gap-1">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            disabled={i === 0}
                            aria-label={t('pricing:bands.moveUp', { n: i + 1 })}
                            onClick={() => move(i, -1)}
                          >
                            <ArrowUpIcon />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            disabled={i === doc.bands.length - 1}
                            aria-label={t('pricing:bands.moveDown', { n: i + 1 })}
                            onClick={() => move(i, 1)}
                          >
                            <ArrowDownIcon />
                          </Button>
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon"
                            aria-label={t('pricing:bands.remove', { n: i + 1 })}
                            onClick={() =>
                              onChange({ ...doc, bands: doc.bands.filter((_, j) => j !== i) })
                            }
                          >
                            <Trash2Icon />
                          </Button>
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>
      {readOnly ? null : (
        <div>
          <Button type="button" variant="outline" onClick={add}>
            <PlusIcon />
            {t('pricing:bands.add')}
          </Button>
        </div>
      )}
    </div>
  );
}
