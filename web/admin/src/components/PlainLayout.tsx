import { Link, Outlet } from '@tanstack/react-router';

import { BrandLogo } from '@porto/shared/ui/patterns';

import { UserMenu } from './UserMenu';

/** Layout for pages outside a marina: picker, account, organization. */
export function PlainLayout() {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="flex items-center justify-between border-b px-4 py-2">
        <Link to="/">
          <BrandLogo variant="horizontal" size="sm" />
        </Link>
        <UserMenu />
      </header>
      <main className="mx-auto w-full max-w-5xl flex-1 p-6">
        <Outlet />
      </main>
    </div>
  );
}
