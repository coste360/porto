import { Link } from '@tanstack/react-router';
import { ChevronsUpDownIcon, LogOutIcon, MonitorIcon, UserIcon } from 'lucide-react';
import { useState } from 'react';

import { SUPPORTED_LOCALES, useT } from '@porto/shared/i18n';
import { unsentOps } from '@porto/shared/offline';
import { useTheme, type ThemePreference } from '@porto/shared/theme';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@porto/shared/ui/alert-dialog';
import { Avatar, AvatarFallback } from '@porto/shared/ui/avatar';
import { Button, buttonVariants } from '@porto/shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@porto/shared/ui/dropdown-menu';

import { useLogout, useMe } from '../auth/hooks';
import { useDesktopAccess } from '../features/desktop/lib';
import { initials } from '../lib/text';

const THEMES: { value: ThemePreference; key: string }[] = [
  { value: 'light', key: 'common:user.themeLight' },
  { value: 'dark', key: 'common:user.themeDark' },
  { value: 'system', key: 'common:user.themeSystem' },
];

/** User menu: account, theme, language, sign out. `trigger` lets the sidebar render it. */
export function UserMenu({ variant = 'button' }: { variant?: 'button' | 'sidebar' }) {
  const { t, i18n } = useT();
  const { data: me } = useMe();
  const { theme, setTheme } = useTheme();
  const logout = useLogout();
  const desktop = useDesktopAccess();
  // Unsent offline operations are lost on logout (the offline database is deleted): ask first (m6b-plan §4.3).
  const [unsent, setUnsent] = useState(0);
  if (!me) return null;
  const name = me.user.name || me.user.email;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className={
            variant === 'sidebar'
              ? 'h-auto w-full justify-start gap-2 px-2 py-2 group-data-[collapsible=icon]:px-0'
              : 'gap-2'
          }
        >
          {/* Visible text stays part of the accessible name (WCAG 2.5.3), so no aria-label here. */}
          <span className="sr-only">{t('common:user.menu')}</span>
          <Avatar className="size-8">
            <AvatarFallback className="bg-primary text-primary-foreground">
              {initials(name)}
            </AvatarFallback>
          </Avatar>
          {variant === 'sidebar' ? (
            <>
              <span className="flex min-w-0 flex-1 flex-col text-left group-data-[collapsible=icon]:hidden">
                <span className="truncate text-sm font-medium">{name}</span>
                <span className="truncate text-xs opacity-80">{me.user.email}</span>
              </span>
              <ChevronsUpDownIcon className="size-4 group-data-[collapsible=icon]:hidden" />
            </>
          ) : null}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="truncate">{name}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem asChild>
            <Link to="/account">
              <UserIcon />
              {t('common:nav.account')}
            </Link>
          </DropdownMenuItem>
          {desktop.admin ? (
            <DropdownMenuItem asChild>
              <Link to="/desktop">
                <MonitorIcon />
                {t('desktop:appName')}
              </Link>
            </DropdownMenuItem>
          ) : null}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>{t('common:user.theme')}</DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={theme}
                onValueChange={(value) => setTheme(value as ThemePreference)}
              >
                {THEMES.map((item) => (
                  <DropdownMenuRadioItem key={item.value} value={item.value}>
                    {t(item.key)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>{t('common:user.language')}</DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              <DropdownMenuRadioGroup
                value={i18n.language}
                onValueChange={(value) => void i18n.changeLanguage(value)}
              >
                {SUPPORTED_LOCALES.map((locale) => (
                  <DropdownMenuRadioItem key={locale} value={locale}>
                    {t(`common:locales.${locale}`)}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            const n = unsentOps(me.user.id).length;
            if (n > 0) setUnsent(n);
            else logout.mutate();
          }}
        >
          <LogOutIcon />
          {t('common:user.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
      <AlertDialog open={unsent > 0} onOpenChange={(o) => !o && setUnsent(0)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('offline:logoutConfirm.title')}</AlertDialogTitle>
            <AlertDialogDescription>
              {unsent === 1
                ? t('offline:logoutConfirm.description_one', { count: 1 })
                : t('offline:logoutConfirm.description_other', { count: unsent })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('common:actions.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className={buttonVariants({ variant: 'destructive' })}
              onClick={() => logout.mutate()}
            >
              {t('offline:logoutConfirm.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </DropdownMenu>
  );
}
