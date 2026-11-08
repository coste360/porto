import { lazy } from 'react';

import { BookingPage } from './BookingPage';
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
    default:
      return <BookingPage slug={route.slug} />;
  }
}
