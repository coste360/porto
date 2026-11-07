import { getRouteApi } from '@tanstack/react-router';
import { PrinterIcon } from 'lucide-react';

import {
  usePriceMatrix,
  usePricingToolPriceList,
  type ContractType,
  type PriceListDocument,
} from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { formatIsoDate } from '@porto/shared/lib/format';
import { Button } from '@porto/shared/ui/button';
import { BrandLogo } from '@porto/shared/ui/patterns';
import { printPage } from '@porto/shared/lib/download';

import { useCurrentMarina } from '../../../auth/hooks';
import { LoadingState, QueryErrorState, RequirePermission } from '../../../components/PageStates';
import { localName } from '../../maps/lib';
import { PriceMatrixTable } from '../components/PriceMatrixTable';
import { defaultRows, type MatrixSearch } from './matrixSearch';

const route = getRouteApi('/auth/m/$marinaId/price-lists/$priceListId/matrix/print');

/** Printable matrix of every contract type of a list (A4 landscape), with the Porto mark and the VAT note. */
export function MatrixPrintPage() {
  const { can, marinaId } = useCurrentMarina();
  const { priceListId } = route.useParams();
  const list = usePricingToolPriceList(marinaId, priceListId);
  return (
    <RequirePermission allowed={can('pricing.read')}>
      {list.isPending ? (
        <LoadingState rows={6} />
      ) : list.isError ? (
        <QueryErrorState error={list.error} onRetry={() => void list.refetch()} />
      ) : (
        <PrintSheet doc={list.data} />
      )}
    </RequirePermission>
  );
}

function PrintSheet({ doc }: { doc: PriceListDocument }) {
  const { t, i18n } = useT();
  const { marina } = useCurrentMarina();
  const search = route.useSearch();
  const { contract_type: _ignored, ...params } = search;
  const rows = params.rows ?? defaultRows(doc);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-4 print:hidden">
        <Button onClick={() => void printPage()}>
          <PrinterIcon />
          {t('pricingTools:print.action')}
        </Button>
        <p className="text-sm text-muted-foreground">{t('pricingTools:print.hint')}</p>
      </div>
      <article
        className="flex flex-col gap-6 bg-background text-foreground print:gap-4 print:text-xs"
        aria-label={t('pricingTools:print.sheet')}
        data-testid="matrix-print"
      >
        <header className="flex items-center justify-between gap-4 border-b pb-3">
          <div className="flex flex-col gap-1">
            <h1 className="text-xl font-semibold">
              {t('pricingTools:print.heading', { code: doc.code })}
            </h1>
            <p className="text-sm">
              {localName(doc.name, i18n.language)}
              {marina ? ` · ${marina.name}` : ''}
            </p>
            <p className="text-sm" data-testid="print-validity">
              {t('pricingTools:print.validity', {
                from: formatIsoDate(doc.valid_from),
                to: formatIsoDate(doc.valid_to),
              })}
            </p>
          </div>
          <BrandLogo tone="onLight" size="sm" />
        </header>
        {doc.contract_types.map((c) => (
          <PrintMatrix key={c} doc={doc} contractType={c} params={{ ...params, rows }} />
        ))}
        <footer className="border-t pt-3 text-sm text-muted-foreground print:text-xs">
          <p>{t('pricingTools:print.vatNote')}</p>
          <p>{t('pricingTools:print.engineNote')}</p>
        </footer>
      </article>
    </div>
  );
}

function PrintMatrix({
  doc,
  contractType,
  params,
}: {
  doc: PriceListDocument;
  contractType: ContractType;
  params: MatrixSearch;
}) {
  const { t } = useT();
  const { marinaId } = useCurrentMarina();
  const matrix = usePriceMatrix(marinaId, doc.id, { ...params, contract_type: contractType });
  return (
    <section
      className="flex flex-col gap-2 break-inside-avoid"
      aria-labelledby={`print-${contractType}`}
    >
      <h2 id={`print-${contractType}`} className="text-lg font-semibold print:text-sm">
        {t(`pricingEnums:contractTypes.${contractType}`)}
      </h2>
      {matrix.isPending ? (
        <LoadingState rows={2} />
      ) : matrix.isError ? (
        <QueryErrorState error={matrix.error} />
      ) : (
        <PriceMatrixTable mode="values" matrix={matrix.data} />
      )}
    </section>
  );
}
