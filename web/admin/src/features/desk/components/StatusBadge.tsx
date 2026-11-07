import type { Schemas } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';

import { statusKey, statusVariant } from '../lib/status';

export function StatusBadge({
  kind,
  status,
}: {
  kind: Schemas['ContractKind'];
  status: Schemas['ContractStatus'];
}) {
  const { t } = useT();
  return <Badge variant={statusVariant(status)}>{t(statusKey(kind, status))}</Badge>;
}
