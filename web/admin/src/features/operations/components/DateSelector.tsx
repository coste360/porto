import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react';

import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';
import { Label } from '@porto/shared/ui/label';
import { DatePicker } from '@porto/shared/ui/patterns';

import { addDays } from '../lib/dates';

/** Date of the operational view: today, previous/next night, calendar (spec 9.1). */
export function DateSelector({
  date,
  today,
  onChange,
}: {
  date: string;
  today: string;
  onChange: (date: string) => void;
}) {
  const { t } = useT();
  return (
    <div
      className="flex items-center gap-1"
      role="group"
      aria-label={t('opsMap:date.label')}
      data-testid="ops-date"
      data-date={date}
    >
      <Button
        variant="outline"
        size="icon"
        aria-label={t('opsMap:date.previous')}
        onClick={() => onChange(addDays(date, -1))}
      >
        <ChevronLeftIcon />
      </Button>
      <div className="w-40">
        {/* A label, not aria-label: the button's accessible name must contain the date it shows. */}
        <Label htmlFor="ops-date" className="sr-only">
          {t('opsMap:date.pick')}
        </Label>
        <DatePicker id="ops-date" value={date} onChange={(d) => d && onChange(d)} />
      </div>
      <Button
        variant="outline"
        size="icon"
        aria-label={t('opsMap:date.next')}
        onClick={() => onChange(addDays(date, 1))}
      >
        <ChevronRightIcon />
      </Button>
      <Button
        variant={date === today ? 'secondary' : 'outline'}
        onClick={() => onChange(today)}
        disabled={date === today}
      >
        {t('opsMap:date.today')}
      </Button>
    </div>
  );
}
