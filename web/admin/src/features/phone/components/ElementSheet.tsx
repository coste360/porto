import { BanknoteIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { formatCm } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from '@porto/shared/ui/drawer';

import { elementPresenceAction } from '../lib/element';
import type { MapSelection } from './DayMap';
import type { PresenceTarget } from './PresenceSheet';

export type ElementActions = {
  canPresence: boolean;
  canCollect: boolean;
  onPresence: (target: PresenceTarget) => void;
  onCollect: () => void;
};

/**
 * Bottom sheet of a map element (m6b-plan §6.2): vessel, contract, presence and payment state, and the dock
 * actions (Arrivato / Partito, Incassa) when given; the direction screen shows it read-only.
 */
export function ElementSheet({
  selection,
  onClose,
  actions,
}: {
  selection: MapSelection | null;
  onClose: () => void;
  actions?: ElementActions;
}) {
  const { t, i18n } = useT();
  const s = selection?.subject;
  const el = s?.subject;
  const action = selection && actions?.canPresence ? elementPresenceAction(selection) : null;
  const rows: [string, string][] = [];
  if (el) {
    rows.push([t('phone:dock.scanCard.status'), t(`occupancyEnums:dayStatus.${el.status}`)]);
    if (el.vessel)
      rows.push([
        t('phone:dock.map.contract'),
        [
          el.vessel.name,
          `${formatCm(el.vessel.loa_cm, i18n.language)} × ${formatCm(el.vessel.beam_cm, i18n.language)}`,
          el.contract?.code,
        ]
          .filter(Boolean)
          .join(' · '),
      ]);
    if (el.customer?.display_name)
      rows.push([t('phone:dock.map.customer'), el.customer.display_name]);
    if (el.presence)
      rows.push([
        t('phone:dock.map.presence'),
        t(`occupancyEnums:presenceState.${el.presence.state}`),
      ]);
    if (el.arrival_today) rows.push([t('phone:dock.map.arrivalToday'), t('phone:yes')]);
    if (el.departure_today) rows.push([t('phone:dock.map.departureToday'), t('phone:yes')]);
    if (selection?.payment)
      rows.push([
        t('phone:dock.map.payment'),
        selection.payment.suspended
          ? t('billingEnums:lensState.suspended')
          : t(`billingEnums:lensState.${selection.payment.state}`),
      ]);
  }
  return (
    <Drawer open={selection !== null} onOpenChange={(o) => !o && onClose()}>
      <DrawerContent className="p-4" data-testid="phone-element-sheet">
        <DrawerHeader className="px-0">
          <DrawerTitle>{t('phone:dock.map.elementTitle', { code: s?.code ?? '' })}</DrawerTitle>
          <DrawerDescription>
            {el?.vessel ? el.vessel.name : t('phone:dock.map.free')}
          </DrawerDescription>
        </DrawerHeader>
        <dl className="grid grid-cols-3 gap-x-3 gap-y-2 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd className="col-span-2">{v}</dd>
            </div>
          ))}
        </dl>
        {actions && selection ? (
          <DrawerFooter className="px-0">
            {action && el?.vessel && s?.allocationId ? (
              <Button
                className="min-h-11"
                onClick={() => {
                  onClose();
                  actions.onPresence({
                    vessel: { id: el.vessel!.id, name: el.vessel!.name },
                    allocationId: s.allocationId!,
                    resourceId: s.resourceId,
                    event: action,
                  });
                }}
              >
                {action === 'arrived' ? t('phone:dock.arrived') : t('phone:dock.departed')}
              </Button>
            ) : null}
            {actions.canCollect && el?.contract ? (
              <Button
                variant="outline"
                className="min-h-11"
                onClick={() => {
                  onClose();
                  actions.onCollect();
                }}
              >
                <BanknoteIcon />
                {t('phone:dock.collect')}
              </Button>
            ) : null}
          </DrawerFooter>
        ) : null}
      </DrawerContent>
    </Drawer>
  );
}
