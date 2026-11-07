import { SearchIcon } from 'lucide-react';

import { useOccupancySearch, type OccupancySolution, type Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Button } from '@porto/shared/ui/button';

import { DeskProblemAlert } from '../components/DeskProblemAlert';
import { SolutionList } from './SolutionList';

/** Inline search for an editor (quote option, contract placement): runs the search for the given stay and returns a choice. */
export function SolutionPicker({
  marinaId,
  request,
  onPick,
}: {
  marinaId: string;
  /** The search to run, or null while the editor lacks vessel or dates. */
  request: Schemas['OccupancySearchRequest'] | null;
  onPick: (s: OccupancySolution) => void;
}) {
  const { t } = useT();
  const search = useOccupancySearch(marinaId);
  return (
    <div className="flex flex-col gap-2">
      <div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!request || search.isPending}
          onClick={() => request && search.mutate(request)}
        >
          <SearchIcon />
          {t('desk:search.findPlaces')}
        </Button>
      </div>
      <DeskProblemAlert error={search.error} />
      {search.isSuccess && search.data.solutions.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('desk:search.empty')}</p>
      ) : null}
      {search.isSuccess && search.data.solutions.length > 0 ? (
        <SolutionList
          solutions={search.data.solutions.slice(0, 5)}
          actions={(s) => (
            <Button type="button" size="sm" onClick={() => onPick(s)}>
              {t('desk:search.choose')}
            </Button>
          )}
        />
      ) : null}
    </div>
  );
}
