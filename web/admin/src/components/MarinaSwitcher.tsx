import { Link } from '@tanstack/react-router';
import { BuildingIcon, CheckIcon, ChevronsUpDownIcon } from 'lucide-react';
import { toast } from 'sonner';

import { useT } from '@porto/shared/i18n';
import { canSwitchMarina } from '@porto/shared/offline';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from '@porto/shared/ui/sidebar';

import { useCurrentMarina } from '../auth/hooks';
import { ownedOrganizations } from '../auth/me';

export function MarinaSwitcher() {
  const { t } = useT();
  const { me, marina, marinaId } = useCurrentMarina();
  const orgs = ownedOrganizations(me);
  // Queued offline ops belong to the current marina: leaving it with unsent ones is refused (D9, INV-84).
  const guard = (e: { preventDefault: () => void }, target: string) => {
    if (!me || target === marinaId || canSwitchMarina(me.user.id, marinaId)) return;
    e.preventDefault();
    toast.error(t('offline:marinaSwitchBlocked'));
  };

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton size="lg">
              <BuildingIcon />
              <span className="flex-1 truncate font-medium">{marina?.name}</span>
              <span className="sr-only">{t('common:nav.switchMarina')}</span>
              <ChevronsUpDownIcon />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64">
            <DropdownMenuLabel>{t('common:nav.switchMarina')}</DropdownMenuLabel>
            {me?.marinas.map((m) => (
              <DropdownMenuItem key={m.id} asChild>
                <Link to="/m/$marinaId" params={{ marinaId: m.id }} onClick={(e) => guard(e, m.id)}>
                  <span className="flex-1 truncate">{m.name}</span>
                  {m.id === marinaId ? <CheckIcon /> : null}
                </Link>
              </DropdownMenuItem>
            ))}
            {orgs.length > 0 ? <DropdownMenuSeparator /> : null}
            {orgs.map((org) => (
              <DropdownMenuItem key={org.id} asChild>
                <Link
                  to="/org/$organizationId"
                  params={{ organizationId: org.id }}
                  onClick={(e) => guard(e, '')}
                >
                  {t('common:nav.organization')}: {org.name}
                </Link>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
