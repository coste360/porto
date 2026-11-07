import { PlusIcon, TrashIcon } from 'lucide-react';
import { useState } from 'react';

import type { SignatureRequestInput, SignerRole } from '@porto/shared/api';
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

import { useCurrentMarina } from '../../../auth/hooks';
import { EnumSelect, M4ProblemAlert } from '../../contracts-m4/components/kit';
import { CustomerField } from '../../desk/components/CustomerField';
import { DRIVERS, ORDER, signatureBody, type SignerRow } from '../lib/signature';

const ROLES: readonly SignerRole[] = [
  'customer',
  'co_holder',
  'outgoing_holder',
  'incoming_holder',
  'legal_representative',
  'marina',
];

type Row = SignerRow;

/** Opens a signature request for a contract, an addendum or a transfer (spec 15.4). */
export function RequestSignatureDialog({
  open,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  open: boolean;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onSubmit: (body: SignatureRequestInput) => void;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const [driver, setDriver] = useState<(typeof DRIVERS)[number] | ''>('');
  const [order, setOrder] = useState<(typeof ORDER)[number] | ''>('');
  const [rows, setRows] = useState<Row[]>([]);
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('signature:request.title')}</DialogTitle>
          <DialogDescription>{t('signature:request.description')}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-3">
          <EnumSelect
            id="sig-driver"
            label={t('signature:request.driver')}
            group="signatureDriver"
            values={DRIVERS}
            value={driver}
            onChange={setDriver}
            allLabel={t('signature:request.default')}
          />
          <EnumSelect
            id="sig-order"
            label={t('signature:request.order')}
            group="orderMode"
            values={ORDER}
            value={order}
            onChange={setOrder}
            allLabel={t('signature:request.default')}
          />
        </div>
        <div className="flex flex-col gap-2">
          <p className="text-muted-foreground text-sm">{t('signature:request.signersHint')}</p>
          {rows.map((r, i) => (
            <div key={i} className="flex flex-wrap items-end gap-2">
              <EnumSelect
                id={`sig-role-${i}`}
                label={t('signature:request.role')}
                group="signerRole"
                values={ROLES}
                value={r.role}
                onChange={(role) =>
                  setRows(rows.map((x, j) => (j === i ? { ...x, role: role || 'customer' } : x)))
                }
              />
              {r.role !== 'marina' ? (
                <div className="min-w-56 flex-1">
                  <CustomerField
                    marinaId={marinaId}
                    id={`sig-customer-${i}`}
                    value={r.customer}
                    onChange={(customer) =>
                      setRows(rows.map((x, j) => (j === i ? { ...x, customer } : x)))
                    }
                  />
                </div>
              ) : null}
              <Button
                variant="ghost"
                size="icon"
                aria-label={t('signature:request.removeSigner')}
                onClick={() => setRows(rows.filter((_, j) => j !== i))}
              >
                <TrashIcon />
              </Button>
            </div>
          ))}
          <div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRows([...rows, { role: 'co_holder', customer: null }])}
            >
              <PlusIcon />
              {t('signature:request.addSigner')}
            </Button>
          </div>
        </div>
        <M4ProblemAlert error={error} />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common:actions.close')}
          </Button>
          <Button disabled={pending} onClick={() => onSubmit(signatureBody(driver, order, rows))}>
            {t('signature:request.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
