import { useMemo } from 'react';

import { useItems, useRevenueClasses } from '@porto/shared/api';

export type ItemOption = { id: string; code: string; rental: boolean };

/** The active items of the marina with their rental flag (revenue class rental). */
export function useItemOptions(marinaId: string): { items: ItemOption[]; isPending: boolean } {
  const items = useItems(marinaId, { active: true });
  const classes = useRevenueClasses(marinaId);
  return useMemo(() => {
    const rentalClass = (classes.data?.items ?? []).find((c) => c.code === 'rental')?.id;
    const list = (items.data?.pages ?? []).flatMap((p) => p.items);
    return {
      items: list.map((i) => ({
        id: i.id,
        code: i.code,
        rental: i.revenue_class_id === rentalClass,
      })),
      isPending: items.isPending || classes.isPending,
    };
  }, [items.data, items.isPending, classes.data, classes.isPending]);
}
