import { Link } from '@tanstack/react-router';

import { useT } from '@porto/shared/i18n';

/** Sections of the yard planner (m10-plan §14.3). */
export function YardPlannerNav({
  marinaId,
  current,
}: {
  marinaId: string;
  current: 'planner' | 'load' | 'employees' | 'lifting';
}) {
  const { t } = useT();
  const cls = (on: boolean) =>
    on
      ? 'border-primary text-foreground border-b-2 px-3 py-2 text-sm font-medium'
      : 'text-muted-foreground hover:text-foreground px-3 py-2 text-sm';
  return (
    <nav aria-label={t('yardPlanner:nav.sections')} className="flex flex-wrap gap-1 border-b">
      <Link
        to="/m/$marinaId/yard/planner"
        params={{ marinaId }}
        className={cls(current === 'planner')}
      >
        {t('yardPlanner:nav.planner')}
      </Link>
      <Link to="/m/$marinaId/yard/load" params={{ marinaId }} className={cls(current === 'load')}>
        {t('yardPlanner:nav.load')}
      </Link>
      <Link
        to="/m/$marinaId/yard/employees"
        params={{ marinaId }}
        className={cls(current === 'employees')}
      >
        {t('yardPlanner:nav.employees')}
      </Link>
      <Link
        to="/m/$marinaId/yard/lifting"
        params={{ marinaId }}
        className={cls(current === 'lifting')}
      >
        {t('yardPlanner:nav.lifting')}
      </Link>
    </nav>
  );
}
