import { Link } from '@tanstack/react-router';
import { ChevronsUpDownIcon, LogOutIcon, UserIcon } from 'lucide-react';

import { SUPPORTED_LOCALES, useT } from '@porto/shared/i18n';
import { useTheme, type ThemePreference } from '@porto/shared/theme';
import { Avatar, AvatarFallback } from '@porto/shared/ui/avatar';
import { Button } from '@porto/shared/ui/button';
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
  if (!me) return null;
  const name = me.user.name || me.user.email;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className={
            variant === 'sidebar' ? 'h-auto w-full justify-start gap-2 px-2 py-2' : 'gap-2'
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
              <span className="flex min-w-0 flex-1 flex-col text-left">
                <span className="truncate text-sm font-medium">{name}</span>
                <span className="truncate text-xs opacity-80">{me.user.email}</span>
              </span>
              <ChevronsUpDownIcon className="size-4" />
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
        <DropdownMenuItem onSelect={() => logout.mutate()}>
          <LogOutIcon />
          {t('common:user.logout')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
