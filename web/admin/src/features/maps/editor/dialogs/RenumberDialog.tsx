import { useMemo, useState } from 'react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
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

import type { Command } from '../model/commands';
import type { EditorDocument } from '../model/document';
import {
  previewRenumber,
  renumberCommand,
  type RenumberParams,
  type RenumberScheme,
} from '../model/renumber';

const SCHEMES: readonly RenumberScheme[] = ['continuous', 'odd_left_even_right', 'per_side'];

/** Renumbering of the selected berths with a preview old → new (ids never change). */
export function RenumberDialog({
  open,
  onOpenChange,
  doc,
  selection,
  onCommand,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  doc: EditorDocument;
  selection: ReadonlySet<string>;
  onCommand: (cmd: Command) => void;
}) {
  const { t } = useT();
  const [params, setParams] = useState<RenumberParams>({
    scheme: 'continuous',
    prefix: '',
    start: 1,
    pad: 0,
  });
  const rows = useMemo(
    () => (open ? previewRenumber(doc, selection, params) : []),
    [open, doc, selection, params],
  );
  const conflicts = rows.filter((r) => r.conflict).length;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('editor:renumber.title')}</DialogTitle>
          <DialogDescription>
            {t('editor:renumber.description', { count: rows.length })}
          </DialogDescription>
        </DialogHeader>
        <div className="grid grid-cols-2 gap-3">
          <div className="col-span-2 flex flex-col gap-1">
            <Label htmlFor="renumber-scheme">{t('editor:renumber.scheme')}</Label>
            <Select
              value={params.scheme}
              onValueChange={(v) => setParams({ ...params, scheme: v as RenumberScheme })}
            >
              <SelectTrigger id="renumber-scheme" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SCHEMES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {t(`editor:renumber.schemes.${s}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="renumber-prefix">{t('editor:renumber.prefix')}</Label>
            <Input
              id="renumber-prefix"
              value={params.prefix}
              maxLength={16}
              onChange={(e) => setParams({ ...params, prefix: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="renumber-start">{t('editor:renumber.start')}</Label>
            <Input
              id="renumber-start"
              inputMode="numeric"
              value={String(params.start)}
              onChange={(e) =>
                /^\d*$/.test(e.target.value) &&
                setParams({ ...params, start: Number(e.target.value || 0) })
              }
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="renumber-pad">{t('editor:renumber.pad')}</Label>
            <Input
              id="renumber-pad"
              inputMode="numeric"
              value={String(params.pad)}
              onChange={(e) =>
                /^[0-3]?$/.test(e.target.value) &&
                setParams({ ...params, pad: Number(e.target.value || 0) })
              }
            />
          </div>
        </div>
        <ScrollArea className="h-64 rounded-md border">
          <Table aria-label={t('editor:renumber.preview')}>
            <TableHeader>
              <TableRow>
                <TableHead>{t('editor:renumber.from')}</TableHead>
                <TableHead>{t('editor:renumber.to')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.ref} data-conflict={r.conflict || undefined}>
                  <TableCell>{r.from}</TableCell>
                  <TableCell className={r.conflict ? 'text-destructive font-medium' : undefined}>
                    {r.to}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </ScrollArea>
        {conflicts > 0 ? (
          <p className="text-destructive text-sm">
            {t('editor:renumber.conflicts', { count: conflicts })}
          </p>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={rows.length === 0 || conflicts > 0}
            onClick={() => {
              onCommand(renumberCommand(doc, rows));
              onOpenChange(false);
            }}
          >
            {t('editor:renumber.apply')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
