import { useState } from 'react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Input } from '@porto/shared/ui/input';
import { Label } from '@porto/shared/ui/label';
import { LengthInput } from '@porto/shared/ui/patterns';

import { parseDegreesToDd } from '../model/calibration';

/** Move by typed offsets (m, may be negative) and rotate by typed degrees. */
export function NumericMoveDialog({
  open,
  onOpenChange,
  onApply,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onApply: (dx: number, dy: number, dd: number) => void;
}) {
  const { t, i18n } = useT();
  const [dx, setDx] = useState<number | null>(0);
  const [dy, setDy] = useState<number | null>(0);
  const [deg, setDeg] = useState('0');
  const dd = parseDegreesToDd(deg);
  const m = t('maps:units.m');
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t('editor:numericMove.title')}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="move-dx">{t('editor:numericMove.dx')}</Label>
            <LengthInput id="move-dx" value={dx} onChange={setDx} unit={m} locale={i18n.language} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="move-dy">{t('editor:numericMove.dy')}</Label>
            <LengthInput id="move-dy" value={dy} onChange={setDy} unit={m} locale={i18n.language} />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="move-rotate">{t('editor:numericMove.rotate')}</Label>
            <Input
              id="move-rotate"
              inputMode="decimal"
              value={deg}
              onChange={(e) => setDeg(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common:actions.cancel')}
          </Button>
          <Button
            disabled={dx === null || dy === null || dd === null}
            onClick={() => {
              onApply(dx ?? 0, dy ?? 0, dd ?? 0);
              onOpenChange(false);
            }}
          >
            {t('editor:numericMove.apply')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
