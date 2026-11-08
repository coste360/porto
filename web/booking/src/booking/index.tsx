// Booking flow of the public page (m8-plan §12.1): steps "Quando e barca", "Posto", "Extra e riepilogo", "Dati",
// "Pagamento", each a URL hash so that back and forward work. The steps after the hold are separate chunks.
import { useQueryClient } from '@tanstack/react-query';
import { RotateCcwIcon, TimerOffIcon } from 'lucide-react';
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react';

import {
  isProblem,
  newIdempotencyKey,
  queryKeys,
  useAbandonBooking,
  useBooking,
  useBookingAvailability,
  useBookingConfig,
  useCreateBooking,
  type BookingConfig,
  type BookingRef,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { cn } from '@porto/shared/lib/utils';
import { Alert, AlertDescription, AlertTitle } from '@porto/shared/ui/alert';
import { Button } from '@porto/shared/ui/button';
import { ProblemAlert } from '@porto/shared/ui/patterns/ProblemAlert';
import { Skeleton } from '@porto/shared/ui/skeleton';

import { Confirmation } from '../common/Confirmation';
import { outcomeOf } from '../common/outcome';
import { Countdown } from '../common/Countdown';
import { useEmbed } from '../common/useEmbed';
import { BerthStep } from './BerthStep';
import { ExtrasStep } from './ExtrasStep';
import { effectiveServices } from './quote';
import { SearchStep } from './SearchStep';
import {
  clampStep,
  clearState,
  emptyState,
  loadState,
  saveState,
  searchParams,
  STEPS,
  stepFromHash,
  type FlowState,
  type Step,
} from './state';

const DetailsStep = lazy(() => import('./DetailsStep'));
const PaymentStep = lazy(() => import('./PaymentStep'));

function StepSkeleton() {
  return <Skeleton className="h-64 w-full" />;
}

export default function BookingFlow({ slug }: { slug: string }) {
  const config = useBookingConfig(slug);
  if (config.isPending) return <StepSkeleton />;
  if (config.isError) return <ProblemAlert error={config.error} />;
  return <Flow slug={slug} config={config.data} />;
}

function useHash(): string {
  const [hash, setHash] = useState(() => window.location.hash);
  useEffect(() => {
    const on = () => setHash(window.location.hash);
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  return hash;
}

function StepNav({ current, max, onGo }: { current: Step; max: Step; onGo: (s: Step) => void }) {
  const { t } = useT();
  const maxIdx = STEPS.indexOf(max);
  const curIdx = STEPS.indexOf(current);
  return (
    <nav aria-label={t('checkout:steps.label')}>
      <ol className="flex gap-1">
        {STEPS.map((s, i) => (
          <li key={s} className="min-w-0 flex-1">
            <button
              type="button"
              disabled={i > maxIdx || i === curIdx}
              aria-current={i === curIdx ? 'step' : undefined}
              onClick={() => onGo(s)}
              className="flex w-full flex-col items-start gap-1 text-left disabled:cursor-default"
            >
              <span
                className={cn('h-1 w-full rounded-full', i <= curIdx ? 'bg-primary' : 'bg-muted')}
              />
              <span
                className={cn(
                  'truncate text-xs',
                  i === curIdx ? 'font-medium' : 'text-muted-foreground',
                  i === curIdx ? '' : 'sr-only sm:not-sr-only',
                )}
              >
                {t(`checkout:steps.${s}`)}
              </span>
            </button>
          </li>
        ))}
      </ol>
    </nav>
  );
}

function Flow({ slug, config }: { slug: string; config: BookingConfig }) {
  const { t, i18n } = useT();
  const qc = useQueryClient();
  const { messenger, embedded } = useEmbed();
  const [state, setState] = useState<FlowState>(() => loadState(slug));
  const [notice, setNotice] = useState<string | null>(null);
  const [expired, setExpired] = useState(false);
  const hash = useHash();
  const flowTop = useRef<HTMLDivElement>(null);

  const update = useCallback(
    (patch: Partial<FlowState>) =>
      setState((s) => {
        const next = { ...s, ...patch };
        saveState(slug, next);
        return next;
      }),
    [slug],
  );
  const go = useCallback((s: Step) => {
    if (window.location.hash !== `#${s}`) window.location.hash = s;
  }, []);

  const step = clampStep(stepFromHash(hash), state);
  useEffect(() => {
    if (window.location.hash !== `#${step}`) {
      window.history.replaceState(window.history.state, '', `#${step}`);
    }
  }, [step]);
  useEffect(() => {
    messenger.post({ type: 'porto:step', step });
    if (!embedded) flowTop.current?.scrollIntoView?.({ block: 'start' });
  }, [step, messenger, embedded]);

  const bookingRef: BookingRef | null = state.booking
    ? { slug, bookingId: state.booking.id, token: state.booking.token }
    : null;
  const booking = useBooking(bookingRef);
  const abandon = useAbandonBooking();
  const create = useCreateBooking(slug);

  const status = booking.data?.status;
  const outcome = booking.data ? outcomeOf(booking.data.status, false) : null;
  const finished = outcome === 'confirmed' || outcome === 'received';
  const lost = status === 'cancelled' || status === 'expired' || status === 'no_show';

  const bookedSent = useRef<string | null>(null);
  useEffect(() => {
    if (outcome === 'confirmed' && booking.data && bookedSent.current !== booking.data.reference) {
      bookedSent.current = booking.data.reference;
      messenger.post({ type: 'porto:booked', reference: booking.data.reference });
    }
  }, [outcome, booking.data, messenger]);

  const availability = useBookingAvailability(
    slug,
    state.search && step !== 'search' ? searchParams(state.search) : null,
  );

  const restart = () => {
    setExpired(false);
    setNotice(null);
    update({ booking: null, createKey: null, payKey: null, category: null });
    void qc.invalidateQueries({ queryKey: queryKeys.publicBooking(slug) });
    go(state.search ? 'berth' : 'search');
  };
  const startOver = () => {
    clearState(slug);
    setState(emptyState);
    setExpired(false);
    setNotice(null);
    go('search');
  };

  const hold = () => {
    if (!state.search || !state.category) return;
    if (state.booking && bookingRef && (status === 'held' || status === undefined)) {
      abandon.mutate(bookingRef);
    }
    const key = state.createKey ?? newIdempotencyKey();
    if (!state.createKey) update({ createKey: key });
    const services = effectiveServices(config.services, state.services, state.servicesTouched);
    create.mutate(
      {
        idempotencyKey: key,
        body: {
          arrival: state.search.arrival,
          departure: state.search.departure,
          category: state.category,
          vessel: {
            loa_cm: state.search.loa_cm,
            beam_cm: state.search.beam_cm,
            ...(state.search.draft_cm ? { draft_cm: state.search.draft_cm } : {}),
            ...(state.search.hull_type ? { hull_type: state.search.hull_type } : {}),
          },
          services: Object.entries(services)
            .filter(([, q]) => q > 0)
            .map(([item_id, qty_milli]) => ({ item_id, qty_milli })),
          ...(state.promo ? { promo_code: state.promo } : {}),
          locale: i18n.language.startsWith('en') ? 'en' : 'it',
          ...(messenger.origin ? { embed_parent_origin: messenger.origin } : {}),
        },
      },
      {
        onSuccess: (b) => {
          setExpired(false);
          update({
            booking: { id: b.booking_id, token: b.booking_token, reference: b.reference },
            createKey: null,
            payKey: null,
          });
          go('details');
        },
        onError: (e) => {
          if (isProblem(e, 'booking_unavailable')) {
            setNotice(t('checkout:berth.taken'));
            update({ createKey: null, category: null });
            void availability.refetch();
            go('berth');
          } else if (isProblem(e) && e.status >= 400 && e.status < 500) {
            update({ createKey: null });
          }
        },
      },
    );
  };

  if (finished && booking.data) {
    return (
      <Confirmation
        outcome={outcome}
        reference={booking.data.reference}
        stay={{ arrival: booking.data.arrival, departure: booking.data.departure }}
        arrivalInfo={config.arrival_info}
        actions={
          <Button variant="ghost" onClick={startOver}>
            {t('checkout:outcome.newBooking')}
          </Button>
        }
      />
    );
  }

  const held = booking.data && (status === 'held' || status === 'pending_payment');
  // A booking the server no longer knows (expired token, another marina) reads as an expired hold.
  const gone = isProblem(booking.error) && booking.error.status === 404;
  const showExpired = expired || lost || gone;

  return (
    <div ref={flowTop} className="flex flex-col gap-4" data-testid="booking-flow" data-step={step}>
      <StepNav current={step} max={clampStep('payment', state)} onGo={go} />
      <h2 className="text-xl font-semibold">{t(`checkout:steps.${step}`)}</h2>

      {showExpired ? (
        <Alert variant="destructive" data-testid="hold-expired">
          <TimerOffIcon />
          <AlertTitle>{t('checkout:hold.expiredTitle')}</AlertTitle>
          <AlertDescription className="flex flex-col gap-2">
            <span>{t('checkout:hold.expiredDescription')}</span>
            <Button variant="outline" size="sm" className="w-fit" onClick={restart}>
              <RotateCcwIcon />
              {t('checkout:hold.searchAgain')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      {held && booking.data?.hold_expires_at && (step === 'details' || step === 'payment') ? (
        <Countdown expiresAt={booking.data.hold_expires_at} onExpire={() => setExpired(true)} />
      ) : null}

      {step === 'search' ? (
        <SearchStep
          config={config}
          initial={state.search}
          onSearch={(s) => {
            setNotice(null);
            update({ search: s, category: null });
            go('berth');
          }}
        />
      ) : null}

      {step === 'berth' && state.search ? (
        <BerthStep
          config={config}
          search={state.search}
          availability={availability.data}
          error={availability.error}
          loading={availability.isPending || availability.isFetching}
          notice={notice}
          onEditSearch={() => go('search')}
          onSelect={(c) => {
            setNotice(null);
            update({ category: c });
            go('extras');
          }}
        />
      ) : null}

      {step === 'extras' && state.search && state.category ? (
        <ExtrasStep
          slug={slug}
          config={config}
          search={state.search}
          category={state.category}
          services={state.services}
          servicesTouched={state.servicesTouched}
          promo={state.promo}
          holding={create.isPending}
          holdError={isProblem(create.error, 'booking_unavailable') ? null : create.error}
          onServices={(s) => update({ services: s, servicesTouched: true })}
          onPromo={(p) => update({ promo: p })}
          onHold={hold}
        />
      ) : null}

      {(step === 'details' || step === 'payment') && bookingRef ? (
        booking.isPending ? (
          <StepSkeleton />
        ) : booking.data && held && !showExpired ? (
          <Suspense fallback={<StepSkeleton />}>
            {step === 'details' ? (
              <DetailsStep
                config={config}
                booking={booking.data}
                bookingRef={bookingRef}
                hullType={state.search?.hull_type}
                onDone={() => go('payment')}
                onHoldExpired={() => setExpired(true)}
              />
            ) : (
              <PaymentStep
                config={config}
                booking={booking.data}
                bookingRef={bookingRef}
                payKey={
                  state.payKey ?? (state.booking ? `pay-${state.booking.id}` : newIdempotencyKey())
                }
                onPayKey={() => update({ payKey: newIdempotencyKey() })}
                onSubmitted={() => void booking.refetch()}
                onHoldExpired={() => setExpired(true)}
              />
            )}
          </Suspense>
        ) : booking.error && !isProblem(booking.error) ? (
          <ProblemAlert error={booking.error} />
        ) : null
      ) : null}
    </div>
  );
}
