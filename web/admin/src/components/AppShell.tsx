import { useQueryClient } from '@tanstack/react-query';
import { Link, Outlet, useRouterState } from '@tanstack/react-router';
import { useEffect } from 'react';

import { subscribeMarinaStream } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { BrandLogo } from '@porto/shared/ui/patterns';
import { Separator } from '@porto/shared/ui/separator';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from '@porto/shared/ui/sidebar';

import { useCurrentMarina } from '../auth/hooks';
import { UPCOMING_MODULES, visibleNavItems } from '../nav';
import { MarinaSwitcher } from './MarinaSwitcher';
import { UserMenu } from './UserMenu';

export function AppShell() {
  const { t } = useT();
  const queryClient = useQueryClient();
  const { marinaId, marina } = useCurrentMarina();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const items = visibleNavItems(marina?.permissions ?? []);

  // Realtime: SSE topics invalidate the marina and team queries (ids only, no PII).
  useEffect(() => subscribeMarinaStream(queryClient, marinaId), [queryClient, marinaId]);

  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <div className="flex items-center px-1 group-data-[collapsible=icon]:hidden">
            <BrandLogo variant="horizontal" size="sm" />
          </div>
          <div className="hidden group-data-[collapsible=icon]:flex">
            <BrandLogo variant="symbol" size="sm" />
          </div>
          <MarinaSwitcher />
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupLabel>{t('common:nav.main')}</SidebarGroupLabel>
            <SidebarMenu>
              {items.map((item) => {
                const href = item.to.replace('$marinaId', marinaId);
                const active = pathname.replace(/\/$/, '').endsWith(href);
                return (
                  <SidebarMenuItem key={item.id}>
                    <SidebarMenuButton asChild isActive={active} tooltip={t(item.labelKey)}>
                      <Link to={item.to} params={{ marinaId }}>
                        <span>{t(item.labelKey)}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroup>
          <SidebarGroup>
            <SidebarGroupLabel>{t('common:nav.upcoming')}</SidebarGroupLabel>
            <SidebarMenu>
              {UPCOMING_MODULES.map((key) => (
                <SidebarMenuItem key={key}>
                  <SidebarMenuButton
                    disabled
                    aria-disabled="true"
                    tooltip={t('common:nav.upcomingHint')}
                    title={t('common:nav.upcomingHint')}
                  >
                    <span>{t(key)}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroup>
        </SidebarContent>
        <SidebarFooter>
          <UserMenu variant="sidebar" />
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 items-center gap-2 border-b px-4">
          <SidebarTrigger aria-label={t('common:nav.openMenu')} title={t('common:nav.openMenu')} />
          <Separator orientation="vertical" className="h-4" />
          <span className="truncate text-sm font-medium">{marina?.name}</span>
        </header>
        <div className="flex-1 p-6">
          <Outlet />
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
