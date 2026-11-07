import { useNavigate } from '@tanstack/react-router';

import { useT } from '@porto/shared/i18n';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@porto/shared/ui/dialog';

import { useMarinaToday } from '../../registry/lib/today';
import { quoteDraftFromPrefill } from '../lib/quote';
import type { DeskPrefill } from '../public';
import { QuoteEditor } from './QuoteEditor';

export function NewQuoteDialog({
  marinaId,
  open,
  onOpenChange,
  prefill,
}: {
  marinaId: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
  prefill?: DeskPrefill;
}) {
  const { t } = useT();
  const today = useMarinaToday();
  const navigate = useNavigate();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-h-screen overflow-y-auto sm:max-w-3xl"
        data-testid="desk-quote-dialog"
      >
        <DialogHeader>
          <DialogTitle>{t('desk:quote.newTitle')}</DialogTitle>
        </DialogHeader>
        {open ? (
          <QuoteEditor
            marinaId={marinaId}
            initial={quoteDraftFromPrefill(prefill, today)}
            onCancel={() => onOpenChange(false)}
            onSaved={(q) => {
              onOpenChange(false);
              void navigate({
                to: '/m/$marinaId/quotes/$quoteId',
                params: { marinaId, quoteId: q.id },
              });
            }}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
