import type { ReactNode } from 'react';

import { useT } from '@porto/shared/i18n';
import { TabsList, TabsTrigger } from '@porto/shared/ui/tabs';

// Written in full so the Tailwind scanner sees them.
const COLS: Record<number, string> = {
  2: 'grid-cols-2',
  3: 'grid-cols-3',
  4: 'grid-cols-4',
  5: 'grid-cols-5',
  6: 'grid-cols-6',
};

export type PhoneTab<T extends string> = { value: T; label: string; icon: ReactNode };

/** Bottom tab bar of a phone screen (inside shadcn Tabs): fixed, full width, 44 px touch targets. */
export function PhoneTabBar<T extends string>({ tabs }: { tabs: readonly PhoneTab<T>[] }) {
  const { t } = useT();
  return (
    <TabsList
      aria-label={t('phone:tabsLabel')}
      className={`bg-background border-border fixed inset-x-0 bottom-0 z-40 grid h-16 w-full rounded-none border-t ${COLS[tabs.length] ?? 'grid-cols-5'}`}
    >
      {tabs.map((tab) => (
        <TabsTrigger
          key={tab.value}
          value={tab.value}
          className="flex min-h-11 flex-col gap-0 text-xs"
        >
          {tab.icon}
          {tab.label}
        </TabsTrigger>
      ))}
    </TabsList>
  );
}
