import { lazy } from 'react';

import { BookingPage } from './BookingPage';
import { ProfileLink } from './profile/ProfileLink';
import type { BookingRoute } from './routes';

// Lazy chunks (m8-plan §12.1 budget): the entry carries only the marina page shell; the booking flow (inside
// BookingPage), the M4 link pages, the M5 pay page, the return page and the customer area load on their routes.
const LinkPage = lazy(() => import('./links').then((m) => ({ default: m.LinkPage })));
const PayPage = lazy(() => import('./pay').then((m) => ({ default: m.PayPage })));
const AccountPage = lazy(() => import('./account').then((m) => ({ default: m.AccountPage })));
const AccountLinkPage = lazy(() =>
  import('./account').then((m) => ({ default: m.AccountLinkPage })),
);
const ReturnPage = lazy(() => import('./booking/ReturnPage'));
// M12 (m12-plan §12.3): the public profile is its own lazy chunk (the booking first-load budget is unchanged).
const ProfilePage = lazy(() => import('./profile').then((m) => ({ default: m.ProfilePage })));

export function Page({ route }: { route: BookingRoute }) {
  switch (route.page) {
    case 'link':
      return <LinkPage slug={route.slug} purpose={route.purpose} token={route.token} />;
    case 'pay':
      return <PayPage slug={route.slug} token={route.token} ret={route.ret} />;
    case 'return':
      return <ReturnPage slug={route.slug} bookingId={route.bookingId} />;
    case 'account':
      return <AccountPage slug={route.slug} contractId={route.contractId} />;
    case 'accountLink':
      return <AccountLinkPage slug={route.slug} />;
    case 'profile':
      return <ProfilePage slug={route.slug} />;
    default:
      // M12: the "Scheda del porto" link when the marina publishes its profile (only the link is in the entry).
      return (
        <>
          <BookingPage slug={route.slug} />
          <ProfileLink slug={route.slug} />
        </>
      );
  }
}
