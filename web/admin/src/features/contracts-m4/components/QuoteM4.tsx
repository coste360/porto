import { MailIcon } from 'lucide-react';
import { useState } from 'react';

import { useSendQuote, type PlanChoice, type Quote } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatDateTime } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { Checkbox } from '@porto/shared/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@porto/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@porto/shared/ui/field';
import { RadioGroup, RadioGroupItem } from '@porto/shared/ui/radio-group';

import { GeneratedDocuments } from '../../documents/public';
import { M4ProblemAlert } from './kit';

/**
 * Send of a quote (13.3, m4-plan §3.2): the PDF and a magic link by email to the customer; "manual" only marks it
 * as sent (M3 behaviour, e.g. handed over at the desk). Re-sending rotates the link. After the send the quote PDF
 * appears in the documents list.
 */
export function SendQuoteDialog({
  marinaId,
  quote,
  onClose,
}: {
  marinaId: string;
  quote: Quote;
  onClose: () => void;
}) {
  const { t, i18n } = useT();
  const send = useSendQuote(marinaId);
  const [manual, setManual] = useState(false);
  const [sent, setSent] = useState(false);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-screen overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('contractsM4:quote.sendTitle')}</DialogTitle>
          <DialogDescription>
            {quote.sent_at
              ? t('contractsM4:quote.resendDescription', {
                  at: formatDateTime(quote.sent_at, i18n.language),
                })
              : t('contractsM4:quote.sendDescription')}
          </DialogDescription>
        </DialogHeader>
        <Field orientation="horizontal">
          <Checkbox
            id="quote-manual"
            checked={manual}
            onCheckedChange={(c) => setManual(c === true)}
          />
          <FieldLabel htmlFor="quote-manual">{t('contractsM4:quote.manual')}</FieldLabel>
        </Field>
        {sent ? (
          <>
            <p className="text-sm" role="status">
              {manual ? t('contractsM4:quote.markedSent') : t('contractsM4:quote.sent')}
            </p>
            <GeneratedDocuments subjectType="quote" subjectId={quote.id} />
          </>
        ) : null}
        <M4ProblemAlert error={send.error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          {!sent ? (
            <Button
              disabled={send.isPending}
              onClick={() =>
                send.mutate(
                  { id: quote.id, version: quote.version, manual },
                  { onSuccess: () => setSent(true) },
                )
              }
            >
              <MailIcon />
              {manual ? t('contractsM4:quote.markSent') : t('contractsM4:quote.send')}
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Plan choice of a quote acceptance (standard or early payment, D32). */
export function AcceptPlanFields({
  value,
  onChange,
}: {
  value: PlanChoice;
  onChange: (p: PlanChoice) => void;
}) {
  const { t } = useT();
  return (
    <Field>
      <FieldLabel>{t('contractsM4:quote.plan')}</FieldLabel>
      <RadioGroup
        value={value}
        onValueChange={(v) => onChange(v as PlanChoice)}
        aria-label={t('contractsM4:quote.plan')}
      >
        {(['standard', 'early_payment'] as const).map((p) => (
          <Field key={p} orientation="horizontal">
            <RadioGroupItem id={`accept-plan-${p}`} value={p} />
            <FieldLabel htmlFor={`accept-plan-${p}`}>{t(`docsEnums:planChoice.${p}`)}</FieldLabel>
          </Field>
        ))}
      </RadioGroup>
      <FieldDescription>{t('contractsM4:quote.planHint')}</FieldDescription>
    </Field>
  );
}
