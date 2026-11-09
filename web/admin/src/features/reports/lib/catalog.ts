import type { Permission, ServedReportKey } from '@porto/shared/api';

// The report catalogue of the M7 pages (m7-plan §1.1, §3): permission, group_by and sections, the filter controls,
// the totals shown as summary cards, the chart and the record columns of each section. Column keys are the JSON
// field names of the records, in the order of the CSV/Excel export (internal/domain/report/export).

/** How a value is shown. `unit` follows the result's totals.unit (berth-nights, cm-nights, cm²-nights). */
export type CellKind =
  | 'text'
  | 'enum'
  | 'date'
  | 'month'
  | 'cents'
  | 'bp'
  | 'cm'
  | 'cm2'
  | 'int'
  | 'unit'
  | 'unit_area'
  | 'milli'
  | 'bool';

export type ColumnDef = {
  key: string;
  kind: CellKind;
  /** Totals field shown in the Totale row under this column. */
  total?: string;
  /** Translation group of an enumeration value (namespace:prefix). */
  values?: string;
  /** Dotted path of a nested record field (M9 maintenance element.code); default the key. */
  path?: string;
};

export type TotalDef = { key: string; kind: CellKind };

/** Translation group (namespace and key prefix) of enumeration values. */
const vg = (ns: 'reports' | 'billingEnums', prefix: string): string => `${ns}:${prefix}`;

/** Filter controls a report page may show (select of enumerated values, a date, a boolean). */
export type FilterKey =
  | 'variant'
  | 'family'
  | 'map_kind'
  | 'resource_type'
  | 'basis'
  | 'kind'
  | 'channel'
  | 'purpose'
  | 'method'
  | 'origin'
  | 'payment_status'
  | 'invoicing_status'
  | 'fiscal_mode'
  | 'new'
  | 'overdue'
  | 'suggestions'
  | 'as_of'
  // M9 (m9-plan §11)
  | 'exits_blocked_only'
  | 'status'
  | 'element_type'
  | 'overdue_only';

export const FILTER_VALUES: Record<Exclude<FilterKey, 'as_of'>, readonly string[]> = {
  variant: ['physical', 'contractual'],
  family: ['berth', 'land', 'linear_area', 'surface_area'],
  map_kind: ['water', 'yard', 'shed', 'parking', 'boxes', 'other'],
  resource_type: ['berth', 'dynamic_area', 'land_resource'],
  basis: ['issued', 'competence'],
  kind: ['mooring', 'transit', 'charter'],
  channel: ['desk', 'online', 'api', 'external'],
  purpose: ['transit', 'contract'],
  method: [
    'cash',
    'card_pos',
    'bank_transfer',
    'cheque',
    'other',
    'card_online',
    'wallet',
    'sepa_debit',
    'credit_note',
  ],
  origin: [
    'installment',
    'holder_installment',
    'transit',
    'contract_trueup',
    'counter_sale',
    'termination',
    'deposit_retention',
    'manual',
    'adjustment',
    'opening_balance',
    'utility',
  ],
  payment_status: ['open', 'partially_paid', 'paid'],
  invoicing_status: ['to_invoice', 'exported', 'invoiced'],
  fiscal_mode: ['invoice', 'receipt'],
  new: ['true'],
  overdue: ['true'],
  suggestions: ['false'],
  exits_blocked_only: ['true'],
  // The values of status depend on the report (ReportDef.filterValues).
  status: [],
  element_type: ['berth', 'structure', 'dynamic_area', 'land_resource', 'pedestal', 'equipment'],
  overdue_only: ['true'],
};

/** Translation group of the values of each filter. */
export const FILTER_VALUE_GROUP: Record<Exclude<FilterKey, 'as_of'>, string> = {
  variant: vg('reports', 'values.variant'),
  family: vg('reports', 'values.family'),
  map_kind: vg('reports', 'values.mapKind'),
  resource_type: vg('reports', 'values.resourceType'),
  basis: vg('reports', 'values.basis'),
  kind: vg('reports', 'values.contractKind'),
  channel: vg('reports', 'values.channel'),
  purpose: vg('reports', 'values.purpose'),
  method: vg('billingEnums', 'paymentMethod'),
  origin: vg('billingEnums', 'originType'),
  payment_status: vg('billingEnums', 'paymentStatus'),
  invoicing_status: vg('billingEnums', 'invoicingStatus'),
  fiscal_mode: vg('billingEnums', 'fiscalMode'),
  new: vg('reports', 'values.bool'),
  overdue: vg('reports', 'values.bool'),
  suggestions: vg('reports', 'values.bool'),
  exits_blocked_only: vg('reports', 'values.bool'),
  status: vg('reports', 'values.taskStatus'),
  element_type: vg('reports', 'values.elementType'),
  overdue_only: vg('reports', 'values.bool'),
};

export type ReportDef = {
  key: ServedReportKey;
  group: 'operational' | 'economic';
  /** Needs reports.all (money); otherwise reports.operational is enough (m7-plan D17). */
  needsAll: boolean;
  groupBy: readonly string[];
  sections?: readonly string[];
  filters: readonly FilterKey[];
  /** Map, structure, group and category dimensions (m7-plan §3.0). */
  dims?: boolean;
  totals: readonly TotalDef[];
  /** The value charted per group (from the group totals). */
  chart?: TotalDef;
  columns: (section: string) => readonly ColumnDef[];
  /** Report-specific values of a filter (M9: kind = meter kinds, status = task or consumption states). */
  filterValues?: Partial<Record<FilterKey, { values: readonly string[]; group: string }>>;
  /** Search key of the section (M10 yard: `view`); default `section`. */
  sectionParam?: string;
  /** group_by values of a section (M10 yard: per view); default `groupBy`. */
  groupByOf?: (section: string) => readonly string[];
  /** Summary totals of a section (M10 yard: per view); default `totals`. */
  totalsOf?: (section: string) => readonly TotalDef[];
  /** Permission besides reports.all (M10 yard: reports.yard, D32); default reports.operational unless needsAll. */
  permission?: Permission;
};

const E = {
  role: vg('reports', 'values.role'),
  occKind: vg('reports', 'values.occupancyKind'),
  resourceType: vg('reports', 'values.resourceType'),
  contractKind: vg('reports', 'values.contractKind'),
  contractStatus: vg('reports', 'values.contractStatus'),
  term: vg('reports', 'values.term'),
  channel: vg('reports', 'values.channel'),
  purpose: vg('reports', 'values.purpose'),
  outcome: vg('reports', 'values.outcome'),
  source: vg('reports', 'values.source'),
  lengthBand: vg('reports', 'values.lengthBand'),
  seniorityBand: vg('reports', 'values.seniorityBand'),
  hullType: vg('reports', 'values.hullType'),
  event: vg('reports', 'values.event'),
  runKind: vg('reports', 'values.runKind'),
  rightsKind: vg('reports', 'values.rightsKind'),
  receivableKind: vg('reports', 'values.receivableKind'),
  paymentsKind: vg('reports', 'values.paymentsKind'),
  sessionStatus: vg('billingEnums', 'sessionStatus'),
  origin: vg('billingEnums', 'originType'),
  paymentStatus: vg('billingEnums', 'paymentStatus'),
  invoicingStatus: vg('billingEnums', 'invoicingStatus'),
  fiscalMode: vg('billingEnums', 'fiscalMode'),
  method: vg('billingEnums', 'paymentMethod'),
  source2: vg('billingEnums', 'paymentSource'),
  bucket: vg('billingEnums', 'ageingBucket'),
  reminderLevel: vg('billingEnums', 'reminderLevel'),
  mapKind: vg('reports', 'values.mapKind'),
  taskStatus: vg('reports', 'values.taskStatus'),
  priority: vg('reports', 'values.priority'),
  elementType: vg('reports', 'values.elementType'),
  intervention: vg('reports', 'values.intervention'),
  taskSource: vg('reports', 'values.taskSource'),
  meterKind: vg('reports', 'values.meterKind'),
  consumptionStatus: vg('reports', 'values.consumptionStatus'),
  workOrderStatus: vg('reports', 'values.workOrderStatus'),
  bookingPurpose: vg('reports', 'values.bookingPurpose'),
  bookingStatus: vg('reports', 'values.bookingStatus'),
  rentalCategory: vg('reports', 'values.rentalCategory'),
  rentalLineKind: vg('reports', 'values.rentalLineKind'),
  rentalDamageStatus: vg('reports', 'values.rentalDamageStatus'),
  rentalDepositMode: vg('reports', 'values.rentalDepositMode'),
  rentalDepositStatus: vg('reports', 'values.rentalDepositStatus'),
} as const;

const c = (key: string, kind: CellKind, extra: Partial<ColumnDef> = {}): ColumnDef => ({
  key,
  kind,
  ...extra,
});
const en = (key: string, values: string): ColumnDef => ({ key, kind: 'enum', values });
const t = (key: string, kind: CellKind): TotalDef => ({ key, kind });

const chargeColumns = (withMonth: boolean): ColumnDef[] => [
  ...(withMonth ? [c('month', 'month')] : []),
  c('issued_on', 'date'),
  c('number', 'text'),
  c('seq', 'int'),
  c('due_on', 'date'),
  en('origin_type', E.origin),
  c('customer_name', 'text'),
  c('item_code', 'text'),
  c('description', 'text'),
  c('revenue_class', 'text'),
  c('vat_rate_bp', 'bp'),
  c('vat_nature', 'text'),
  c('service_from', 'date'),
  c('service_to', 'date'),
  c('net_cents', 'cents', { total: 'net_cents' }),
  ...(withMonth
    ? []
    : [
        c('vat_cents', 'cents', { total: 'vat_cents' }),
        c('gross_cents', 'cents', { total: 'gross_cents' }),
        en('payment_status', E.paymentStatus),
        en('invoicing_status', E.invoicingStatus),
        en('fiscal_mode', E.fiscalMode),
      ]),
];

export const REPORTS: readonly ReportDef[] = [
  {
    key: 'occupancy',
    group: 'operational',
    needsAll: false,
    groupBy: ['resource', 'structure', 'group', 'category', 'map', 'month', 'week', 'night'],
    filters: ['variant', 'family', 'map_kind', 'resource_type'],
    dims: true,
    totals: [
      t('occupancy_bp', 'bp'),
      t('occupied_units', 'unit'),
      t('available_units', 'unit'),
      t('closed_units', 'unit'),
      t('out_of_service_units', 'unit'),
      t('released_units', 'unit'),
    ],
    chart: t('occupancy_bp', 'bp'),
    columns: () => [
      en('role', E.role),
      c('resource_code', 'text'),
      en('resource_type', E.resourceType),
      c('category', 'text'),
      en('kind', E.occKind),
      c('contract_number', 'text'),
      c('occupied_units', 'unit', { total: 'occupied_units' }),
      c('available_units', 'unit', { total: 'available_units' }),
      c('closed_units', 'unit', { total: 'closed_units' }),
      c('out_of_service_units', 'unit', { total: 'out_of_service_units' }),
    ],
  },
  {
    key: 'efficiency',
    group: 'operational',
    needsAll: false,
    groupBy: ['resource', 'structure', 'group', 'category', 'map', 'month', 'week', 'night'],
    filters: ['family', 'map_kind'],
    dims: true,
    totals: [
      t('efficiency_bp', 'bp'),
      t('nights', 'int'),
      t('vessel_units', 'unit_area'),
      t('resource_units', 'unit_area'),
    ],
    chart: t('efficiency_bp', 'bp'),
    columns: () => [
      c('resource_code', 'text'),
      en('resource_type', E.resourceType),
      en('kind', E.occKind),
      c('contract_number', 'text'),
      c('vessel_name', 'text'),
      c('loa_cm', 'cm'),
      c('beam_cm', 'cm'),
      c('nights', 'int', { total: 'nights' }),
      c('vessel_units', 'unit_area', { total: 'vessel_units' }),
      c('resource_units', 'unit_area', { total: 'resource_units' }),
      c('efficiency_bp', 'bp', { total: 'efficiency_bp' }),
    ],
  },
  {
    key: 'allocation-waste',
    group: 'operational',
    needsAll: false,
    groupBy: ['kind', 'category', 'map', 'structure', 'group'],
    filters: ['map_kind', 'suggestions'],
    dims: true,
    totals: [
      t('count', 'int'),
      t('nights', 'int'),
      t('lost_cents', 'cents'),
      t('applied_cents', 'cents'),
      t('list_cents', 'cents'),
      t('unpriced', 'int'),
    ],
    chart: t('count', 'int'),
    columns: () => [
      c('berth_code', 'text'),
      c('category', 'text'),
      en('kind', E.occKind),
      c('contract_number', 'text'),
      c('vessel_name', 'text'),
      c('loa_cm', 'cm'),
      c('beam_cm', 'cm'),
      c('berth_length_cm', 'cm'),
      c('berth_width_cm', 'cm'),
      c('fill_bp', 'bp'),
      c('waste_bp', 'bp'),
      c('nights_in_period', 'int', { total: 'nights' }),
      c('applied_cents', 'cents', { total: 'applied_cents' }),
      c('list_cents', 'cents', { total: 'list_cents' }),
      c('lost_cents', 'cents', { total: 'lost_cents' }),
    ],
  },
  {
    key: 'lost-demand',
    group: 'operational',
    needsAll: false,
    groupBy: ['reason', 'month', 'week', 'length_band', 'channel', 'source', 'purpose'],
    filters: ['channel', 'purpose'],
    totals: [
      t('count', 'int'),
      t('nights', 'int'),
      t('value_cents', 'cents'),
      t('unpriced', 'int'),
    ],
    chart: t('count', 'int'),
    columns: () => [
      c('arrival', 'date'),
      c('departure', 'date'),
      c('nights_in_period', 'int', { total: 'nights' }),
      en('outcome', E.outcome),
      en('purpose', E.purpose),
      en('term', E.term),
      en('channel', E.channel),
      en('source', E.source),
      c('loa_cm', 'cm'),
      c('beam_cm', 'cm'),
      en('length_band', E.lengthBand),
      c('priced_category', 'text'),
      c('value_cents', 'cents', { total: 'value_cents' }),
    ],
  },
  {
    key: 'presences',
    group: 'operational',
    needsAll: false,
    groupBy: ['day', 'week', 'month', 'contract_kind'],
    sections: ['movements', 'absences', 'releases'],
    filters: [],
    totals: [
      t('arrivals', 'int'),
      t('departures', 'int'),
      t('absence_nights', 'int'),
      t('releases', 'int'),
      t('released_nights', 'int'),
      t('resold_nights', 'int'),
      t('release_credit_cents', 'cents'),
      t('release_revenue_cents', 'cents'),
    ],
    chart: t('arrivals', 'int'),
    columns: (section) => {
      const contract = [
        c('contract_number', 'text'),
        en('contract_kind', E.contractKind),
        c('vessel_name', 'text'),
        c('berth_code', 'text'),
      ];
      if (section === 'absences')
        return [...contract, c('absence_nights', 'int', { total: 'absence_nights' })];
      if (section === 'releases')
        return [
          ...contract,
          c('release_from', 'date'),
          c('release_to', 'date'),
          c('released_nights', 'int', { total: 'released_nights' }),
          c('resold_nights', 'int', { total: 'resold_nights' }),
          c('credit_cents', 'cents', { total: 'release_credit_cents' }),
          c('revenue_cents', 'cents', { total: 'release_revenue_cents' }),
        ];
      return [c('night', 'date'), en('event', E.event), ...contract];
    },
  },
  {
    key: 'vessels',
    group: 'operational',
    needsAll: false,
    groupBy: ['hull_type', 'length_band', 'flag', 'kind'],
    filters: ['map_kind'],
    dims: true,
    totals: [t('vessels', 'int'), t('avg_loa_cm', 'cm'), t('nights', 'int'), t('loa_sum_cm', 'cm')],
    chart: t('vessels', 'int'),
    columns: () => [
      c('name', 'text'),
      c('nights', 'int', { total: 'nights' }),
      c('loa_cm', 'cm', { total: 'loa_sum_cm' }),
      c('beam_cm', 'cm'),
      en('hull_type', E.hullType),
      c('flag', 'text'),
      en('length_band', E.lengthBand),
      en('kind', E.contractKind),
    ],
  },
  {
    key: 'production',
    group: 'economic',
    needsAll: true,
    groupBy: ['kind', 'term', 'month', 'category', 'channel', 'customer'],
    filters: ['kind'],
    totals: [
      t('competence_cents', 'cents'),
      t('contracts', 'int'),
      t('nights', 'int'),
      t('transit_avg_night_cents', 'cents'),
      t('transits', 'int'),
      t('transit_competence_cents', 'cents'),
    ],
    chart: t('competence_cents', 'cents'),
    columns: () => [
      c('number', 'text'),
      en('kind', E.contractKind),
      en('term', E.term),
      en('status', E.contractStatus),
      en('channel', E.channel),
      c('customer_name', 'text'),
      c('category', 'text'),
      c('starts_on', 'date'),
      c('ends_on', 'date'),
      c('total_cents', 'cents'),
      c('nights_in_period', 'int', { total: 'nights' }),
      c('competence_cents', 'cents', { total: 'competence_cents' }),
    ],
  },
  {
    key: 'charges',
    group: 'economic',
    needsAll: true,
    groupBy: [
      'origin',
      'customer',
      'item',
      'revenue_class',
      'payment_status',
      'invoicing_status',
      'month',
      'vat_rate',
    ],
    filters: ['basis', 'origin', 'payment_status', 'invoicing_status', 'fiscal_mode'],
    totals: [
      t('charged_cents', 'cents'),
      t('gross_cents', 'cents'),
      t('net_cents', 'cents'),
      t('vat_cents', 'cents'),
      t('charges', 'int'),
      t('lines', 'int'),
    ],
    chart: t('gross_cents', 'cents'),
    columns: () => chargeColumns(false),
  },
  {
    key: 'payments',
    group: 'economic',
    needsAll: true,
    groupBy: ['method', 'provider', 'source', 'month', 'customer'],
    sections: ['main', 'payouts'],
    filters: ['method'],
    totals: [
      t('collected_cents', 'cents'),
      t('payments_cents', 'cents'),
      t('refunds_cents', 'cents'),
      t('fees_cents', 'cents'),
      t('payouts_cents', 'cents'),
      t('count', 'int'),
    ],
    chart: t('collected_cents', 'cents'),
    columns: (section) => [
      c('date', 'date'),
      en('kind', E.paymentsKind),
      c('number', 'text'),
      en('method', E.method),
      en('source', E.source2),
      c('provider_code', 'text'),
      c('customer_name', 'text'),
      c('amount_cents', 'cents', {
        total: section === 'payouts' ? 'payouts_cents' : 'collected_cents',
      }),
      c('fee_cents', 'cents', section === 'payouts' ? {} : { total: 'fees_cents' }),
    ],
  },
  {
    key: 'revenue-classes',
    group: 'economic',
    needsAll: true,
    groupBy: [],
    filters: ['basis'],
    totals: [t('net_cents', 'cents'), t('unclassified_cents', 'cents'), t('lines', 'int')],
    chart: t('net_cents', 'cents'),
    columns: () => chargeColumns(true),
  },
  {
    key: 'accruals',
    group: 'economic',
    needsAll: true,
    groupBy: ['section', 'month', 'customer', 'origin'],
    sections: ['competence', 'deferral', 'accrual'],
    filters: [],
    totals: [
      t('competence_net_cents', 'cents'),
      t('competence_gross_cents', 'cents'),
      t('deferral_net_cents', 'cents'),
      t('deferral_gross_cents', 'cents'),
      t('accrual_cents', 'cents'),
      t('closing_date', 'date'),
    ],
    chart: t('competence_gross_cents', 'cents'),
    columns: (section) => {
      if (section === 'accrual')
        return [
          c('date', 'date'),
          c('contract_number', 'text'),
          c('customer_name', 'text'),
          c('matured_cents', 'cents'),
          c('charged_cents', 'cents'),
          c('accrual_cents', 'cents', { total: 'accrual_cents' }),
        ];
      const line = [
        c('date', 'date'),
        c('charge_number', 'text'),
        c('line_seq', 'int'),
        c('customer_name', 'text'),
        c('description', 'text'),
        c('service_from', 'date'),
        c('service_to', 'date'),
        c('amount_net_cents', 'cents'),
        c('amount_gross_cents', 'cents'),
      ];
      if (section === 'deferral')
        return [
          ...line,
          c('deferral_net_cents', 'cents', { total: 'deferral_net_cents' }),
          c('deferral_gross_cents', 'cents', { total: 'deferral_gross_cents' }),
        ];
      return [
        ...line,
        c('competence_net_cents', 'cents', { total: 'competence_net_cents' }),
        c('competence_gross_cents', 'cents', { total: 'competence_gross_cents' }),
      ];
    },
  },
  {
    key: 'receivables',
    group: 'economic',
    needsAll: true,
    groupBy: ['bucket', 'customer', 'origin'],
    sections: ['open', 'future', 'reminders'],
    filters: ['as_of', 'overdue', 'origin'],
    totals: [
      t('open_cents', 'cents'),
      t('overdue_cents', 'cents'),
      t('count', 'int'),
      t('future_cents', 'cents'),
      t('reminders_sent', 'int'),
      t('as_of', 'date'),
    ],
    chart: t('open_cents', 'cents'),
    columns: (section) => {
      if (section === 'future')
        return [
          en('kind', E.receivableKind),
          c('number', 'text'),
          c('customer_name', 'text'),
          c('due_on', 'date'),
          c('residual_cents', 'cents', { total: 'future_cents' }),
        ];
      if (section === 'reminders')
        return [
          c('sent_on', 'date'),
          c('number', 'text'),
          c('customer_name', 'text'),
          en('reminder_level', E.reminderLevel),
        ];
      return [
        c('number', 'text'),
        c('customer_name', 'text'),
        en('origin_type', E.origin),
        c('issued_on', 'date'),
        c('due_on', 'date'),
        c('total_cents', 'cents'),
        c('paid_cents', 'cents'),
        c('credited_cents', 'cents'),
        c('residual_cents', 'cents', { total: 'open_cents' }),
        c('overdue', 'bool'),
        en('bucket', E.bucket),
      ];
    },
  },
  {
    key: 'customers',
    group: 'economic',
    needsAll: true,
    groupBy: ['category', 'nationality', 'seniority', 'channel', 'value_band'],
    sections: ['customers', 'renewals'],
    filters: ['new'],
    totals: [
      t('customers', 'int'),
      t('new_customers', 'int'),
      t('value_cents', 'cents'),
      t('arrears_cents', 'cents'),
      t('renewal_rate_bp', 'bp'),
      t('contracts', 'int'),
      t('renewal_eligible', 'int'),
      t('renewal_renewed', 'int'),
    ],
    chart: t('value_cents', 'cents'),
    columns: (section) =>
      section === 'renewals'
        ? [
            c('contract_number', 'text'),
            c('customer_name', 'text'),
            en('term', E.term),
            c('last_night', 'date'),
            c('renewed', 'int', { total: 'renewal_renewed' }),
          ]
        : [
            c('customer_name', 'text'),
            c('category', 'text'),
            c('nationality', 'text'),
            c('first_contract_on', 'date'),
            en('acquisition_channel', E.channel),
            c('seniority_years', 'int'),
            en('seniority_band', E.seniorityBand),
            c('new', 'int', { total: 'new_customers' }),
            c('value_cents', 'cents', { total: 'value_cents' }),
            c('arrears_cents', 'cents', { total: 'arrears_cents' }),
            c('contracts', 'int', { total: 'contracts' }),
          ],
  },
  {
    key: 'rights',
    group: 'economic',
    needsAll: true,
    groupBy: ['holder', 'item', 'month', 'run_kind'],
    sections: ['holders', 'charges', 'allotments', 'mandates'],
    filters: [],
    totals: [
      t('rights', 'int'),
      t('holders', 'int'),
      t('berths', 'int'),
      t('charges_issued_cents', 'cents'),
      t('charges_collected_cents', 'cents'),
      t('charges_residual_cents', 'cents'),
      t('advance_cents', 'cents'),
      t('balance_cents', 'cents'),
      t('marina_share_cents', 'cents'),
      t('mandate_revenue_cents', 'cents'),
      t('mandate_payout_cents', 'cents'),
    ],
    chart: t('charges_issued_cents', 'cents'),
    columns: (section) => {
      if (section === 'charges')
        return [
          c('issued_on', 'date'),
          c('number', 'text'),
          c('right_number', 'text'),
          c('holder_name', 'text'),
          c('issued_cents', 'cents', { total: 'charges_issued_cents' }),
          c('paid_cents', 'cents', { total: 'charges_collected_cents' }),
          c('residual_cents', 'cents', { total: 'charges_residual_cents' }),
        ];
      if (section === 'allotments')
        return [
          c('date', 'date'),
          en('run_kind', E.runKind),
          c('item_code', 'text'),
          c('right_number', 'text'),
          c('holder_name', 'text'),
          c('amount_cents', 'cents'),
        ];
      if (section === 'mandates')
        return [
          c('date', 'date'),
          en('kind', E.rightsKind),
          c('number', 'text'),
          c('right_number', 'text'),
          c('holder_name', 'text'),
          c('amount_cents', 'cents'),
        ];
      return [
        c('right_number', 'text'),
        c('berth_code', 'text'),
        c('holder_name', 'text'),
        c('share_millis', 'milli'),
      ];
    },
  },
  {
    key: 'cash',
    group: 'economic',
    needsAll: true,
    groupBy: ['day', 'month', 'register', 'method', 'vat_rate'],
    sections: ['sessions', 'fiscal'],
    filters: [],
    totals: [
      t('fiscal_total_cents', 'cents'),
      t('documents', 'int'),
      t('fiscal_void_cents', 'cents'),
      t('sessions', 'int'),
      t('difference_cents', 'cents'),
    ],
    chart: t('fiscal_total_cents', 'cents'),
    columns: (section) =>
      section === 'fiscal'
        ? [
            c('business_date', 'date'),
            c('number', 'text'),
            en('method', E.method),
            c('total_cents', 'cents', { total: 'fiscal_total_cents' }),
            c('void_cents', 'cents', { total: 'fiscal_void_cents' }),
          ]
        : [
            c('business_date', 'date'),
            c('register_code', 'text'),
            en('status', E.sessionStatus),
            c('float_cents', 'cents'),
            c('expected_cents', 'cents'),
            c('counted_cents', 'cents'),
            c('difference_cents', 'cents', { total: 'difference_cents' }),
          ],
  },
  {
    key: 'berth-revenue',
    group: 'economic',
    needsAll: true,
    groupBy: ['structure', 'group', 'category', 'map'],
    filters: ['map_kind'],
    dims: true,
    totals: [
      t('revenue_per_m_cents', 'cents'),
      t('revenue_cents', 'cents'),
      t('length_cm', 'cm'),
      t('berths', 'int'),
    ],
    chart: t('revenue_per_m_cents', 'cents'),
    columns: () => [
      c('berth_code', 'text'),
      c('category', 'text'),
      c('length_cm', 'cm', { total: 'length_cm' }),
      c('occupied_nights', 'int'),
      c('revenue_cents', 'cents', { total: 'revenue_cents' }),
      c('revenue_per_m_cents', 'cents', { total: 'revenue_per_m_cents' }),
    ],
  },
  // M9 (m9-plan §11): storage surfaces, maintenance tasks and utility consumptions (operational; money columns
  // only with reports.all).
  {
    key: 'storage',
    group: 'operational',
    needsAll: false,
    groupBy: ['area', 'map_kind', 'contract_kind', 'month'],
    filters: ['map_kind', 'exits_blocked_only'],
    filterValues: { map_kind: { values: ['yard', 'shed'], group: E.mapKind } },
    totals: [
      t('occupancy_bp', 'bp'),
      t('occupied_m2_nights_milli', 'milli'),
      t('available_m2_nights_milli', 'milli'),
      t('boats', 'int'),
      t('exits_blocked', 'int'),
      t('production_cents', 'cents'),
    ],
    chart: t('occupancy_bp', 'bp'),
    columns: () => [
      c('area_code', 'text'),
      en('map_kind', E.mapKind),
      c('contract_code', 'text'),
      en('contract_kind', E.contractKind),
      c('loa_cm', 'cm'),
      c('beam_cm', 'cm'),
      c('nights', 'int'),
      c('hull_m2_nights_milli', 'milli', { total: 'hull_m2_nights_milli' }),
      c('footprint_m2_nights_milli', 'milli', { total: 'occupied_m2_nights_milli' }),
      c('exit_on', 'date'),
      c('exit_blocked', 'bool', { total: 'exits_blocked' }),
      c('production_cents', 'cents', { total: 'production_cents' }),
    ],
  },
  {
    key: 'maintenance',
    group: 'operational',
    needsAll: false,
    groupBy: ['status', 'priority', 'element_type', 'intervention', 'month'],
    filters: ['status', 'element_type', 'overdue_only'],
    filterValues: {
      status: {
        values: ['open', 'scheduled', 'in_progress', 'done', 'cancelled'],
        group: E.taskStatus,
      },
    },
    totals: [
      t('tasks', 'int'),
      t('overdue', 'int'),
      t('open', 'int'),
      t('in_progress', 'int'),
      t('done', 'int'),
      t('internal_minutes', 'int'),
      t('external_cost_cents', 'cents'),
    ],
    chart: t('tasks', 'int'),
    columns: () => [
      c('element_code', 'text', { path: 'element.code' }),
      { ...en('element_type', E.elementType), path: 'element.element_type' },
      en('intervention', E.intervention),
      en('source', E.taskSource),
      en('priority', E.priority),
      en('status', E.taskStatus),
      c('due_on', 'date'),
      c('done_on', 'date'),
      c('overdue_days', 'int'),
      c('internal_minutes', 'int', { total: 'internal_minutes' }),
      c('external_cost_cents', 'cents', { total: 'external_cost_cents' }),
    ],
  },
  {
    key: 'utilities',
    group: 'operational',
    needsAll: false,
    groupBy: ['kind', 'meter', 'status', 'contract', 'month'],
    filters: ['kind', 'status'],
    filterValues: {
      kind: { values: ['power', 'water', 'hours', 'cycles'], group: E.meterKind },
      status: {
        values: ['pending', 'anomaly', 'unassigned', 'charged', 'excluded'],
        group: E.consumptionStatus,
      },
    },
    totals: [
      t('power_milli', 'milli'),
      t('water_milli', 'milli'),
      t('pending_milli', 'milli'),
      t('unassigned_milli', 'milli'),
      t('charged_cents', 'cents'),
      t('pending_value_cents', 'cents'),
    ],
    chart: t('power_milli', 'milli'),
    columns: () => [
      c('meter_code', 'text'),
      en('kind', E.meterKind),
      en('resource_type', E.resourceType),
      c('contract_code', 'text'),
      c('first_night', 'date'),
      c('last_night', 'date'),
      c('nights', 'int'),
      c('qty_milli', 'milli'),
      en('status', E.consumptionStatus),
      c('charged_cents', 'cents', { total: 'charged_cents' }),
    ],
  },
  // M10 (m10-plan §12): the yard report, one view per section (the `view` parameter); money visible with
  // reports.yard as well as reports.all (D32).
  {
    key: 'yard',
    group: 'operational',
    needsAll: false,
    permission: 'reports.yard',
    sectionParam: 'view',
    sections: ['hours', 'load', 'margins', 'margins_by_work_type', 'lifting'],
    groupBy: [
      'employee',
      'department',
      'order',
      'work_type',
      'qualification',
      'bay',
      'status',
      'equipment',
      'week',
      'month',
    ],
    groupByOf: (view) =>
      ({
        hours: ['employee', 'order', 'work_type', 'qualification', 'month', 'week'],
        load: ['employee', 'department', 'qualification', 'bay', 'week', 'month'],
        margins: ['status', 'month'],
        margins_by_work_type: ['work_type', 'order'],
        lifting: ['equipment', 'status', 'month'],
      })[view] ?? [],
    filters: [],
    totals: [t('records', 'int'), t('hours_milli', 'milli')],
    totalsOf: (view) =>
      ({
        hours: [t('hours_milli', 'milli'), t('cost_cents', 'cents')],
        load: [
          t('capacity_milli', 'milli'),
          t('assigned_milli', 'milli'),
          t('hours_milli', 'milli'),
          t('load_bp', 'bp'),
        ],
        margins: [
          t('records', 'int'),
          t('revenue_cents', 'cents'),
          t('cost_cents', 'cents'),
          t('margin_cents', 'cents'),
          t('margin_bp', 'bp'),
          t('quote_net_cents', 'cents'),
        ],
        margins_by_work_type: [
          t('revenue_cents', 'cents'),
          t('cost_cents', 'cents'),
          t('margin_cents', 'cents'),
          t('margin_bp', 'bp'),
        ],
        lifting: [t('booked_slots', 'int'), t('window_slots', 'int')],
      })[view] ?? [],
    chart: t('records', 'int'),
    columns: (view) => {
      switch (view) {
        case 'load':
          return [
            c('date', 'date'),
            c('employee_code', 'text'),
            c('employee_name', 'text'),
            c('department', 'text'),
            c('bay', 'text'),
            c('capacity_milli', 'milli', { total: 'capacity_milli' }),
            c('assigned_milli', 'milli', { total: 'assigned_milli' }),
            c('actual_milli', 'milli', { total: 'hours_milli' }),
            c('load_bp', 'bp', { total: 'load_bp' }),
            c('overload', 'bool'),
          ];
        case 'margins':
        case 'margins_by_work_type':
          return [
            c('work_order_number', 'text'),
            en('work_order_status', E.workOrderStatus),
            c('work_type_code', 'text'),
            c('revenue_cents', 'cents', { total: 'revenue_cents' }),
            c('cost_cents', 'cents', { total: 'cost_cents' }),
            c('margin_cents', 'cents', { total: 'margin_cents' }),
            c('margin_bp', 'bp', { total: 'margin_bp' }),
            c('quote_net_cents', 'cents', { total: 'quote_net_cents' }),
            c('delta_net_cents', 'cents'),
          ];
        case 'lifting':
          return [
            c('date', 'date'),
            c('equipment_code', 'text'),
            c('from_slot', 'int'),
            c('to_slot', 'int'),
            en('purpose', E.bookingPurpose),
            en('booking_status', E.bookingStatus),
          ];
        default:
          return [
            c('date', 'date'),
            c('employee_code', 'text'),
            c('employee_name', 'text'),
            c('work_order_number', 'text'),
            c('work_type_code', 'text'),
            c('hours_milli', 'milli', { total: 'hours_milli' }),
            c('cost_cents', 'cents', { total: 'cost_cents' }),
          ];
      }
    },
  },
  // M11 (m11-plan §11): the rental report, one view per section (the `view` parameter); money visible with
  // reports.rental as well as reports.all. Deposits are never revenue: retentions appear in damages only.
  {
    key: 'rental',
    group: 'operational',
    needsAll: false,
    permission: 'reports.rental',
    sectionParam: 'view',
    sections: ['utilisation', 'revenue', 'damages', 'deposits'],
    groupBy: ['unit', 'category', 'formula', 'line_kind', 'status', 'mode', 'day', 'week', 'month'],
    groupByOf: (view) =>
      ({
        utilisation: ['unit', 'category', 'day', 'week', 'month'],
        revenue: ['unit', 'category', 'formula', 'line_kind', 'day', 'week', 'month'],
        damages: ['unit', 'category', 'status', 'month'],
        deposits: ['unit', 'category', 'formula', 'mode', 'status', 'month'],
      })[view] ?? [],
    filters: [],
    totals: [t('available_slots', 'int'), t('rented_slots', 'int'), t('utilisation_bp', 'bp')],
    totalsOf: (view) =>
      ({
        utilisation: [
          t('available_slots', 'int'),
          t('rented_slots', 'int'),
          t('utilisation_bp', 'bp'),
        ],
        revenue: [
          t('records', 'int'),
          t('net_cents', 'cents'),
          t('vat_cents', 'cents'),
          t('gross_cents', 'cents'),
        ],
        damages: [t('records', 'int'), t('estimate_cents', 'cents'), t('retained_cents', 'cents')],
        deposits: [
          t('deposit_cents', 'cents'),
          t('retained_cents', 'cents'),
          t('released_cents', 'cents'),
          t('outstanding_cents', 'cents'),
        ],
      })[view] ?? [],
    chart: t('records', 'int'),
    columns: (view) => {
      switch (view) {
        case 'revenue':
          return [
            c('date', 'date'),
            c('booking_number', 'text'),
            c('unit_code', 'text'),
            en('category', E.rentalCategory),
            c('formula_code', 'text'),
            en('line_kind', E.rentalLineKind),
            c('net_cents', 'cents', { total: 'net_cents' }),
            c('vat_cents', 'cents', { total: 'vat_cents' }),
            c('gross_cents', 'cents', { total: 'gross_cents' }),
          ];
        case 'damages':
          return [
            c('date', 'date'),
            c('booking_number', 'text'),
            c('unit_code', 'text'),
            en('damage_status', E.rentalDamageStatus),
            c('estimate_cents', 'cents', { total: 'estimate_cents' }),
            c('retained_cents', 'cents', { total: 'retained_cents' }),
          ];
        case 'deposits':
          return [
            c('date', 'date'),
            c('booking_number', 'text'),
            c('unit_code', 'text'),
            en('deposit_mode', E.rentalDepositMode),
            en('deposit_status', E.rentalDepositStatus),
            c('deposit_cents', 'cents', { total: 'deposit_cents' }),
            c('retained_cents', 'cents', { total: 'retained_cents' }),
            c('released_cents', 'cents', { total: 'released_cents' }),
            c('outstanding_cents', 'cents', { total: 'outstanding_cents' }),
          ];
        default:
          return [
            c('date', 'date'),
            c('unit_code', 'text'),
            en('category', E.rentalCategory),
            c('available_slots', 'int', { total: 'available_slots' }),
            c('rented_slots', 'int', { total: 'rented_slots' }),
            c('utilisation_bp', 'bp', { total: 'utilisation_bp' }),
          ];
      }
    },
  },
  // M12 (m12-plan §10.3): physical vs contractual discrepancies per (resource, night), view discrepancies; codes only
  // (never vessel names), the drill links to the berth history. Operational: reports.operational is enough.
  {
    key: 'sensors',
    group: 'operational',
    needsAll: false,
    sectionParam: 'view',
    sections: ['discrepancies'],
    groupBy: ['resource', 'map', 'group', 'kind', 'night', 'week', 'month'],
    filters: ['kind'],
    dims: true,
    filterValues: {
      kind: {
        values: ['occupied_unexpected', 'absent'],
        group: vg('reports', 'values.sensorDiscrepancy'),
      },
    },
    totals: [
      t('records', 'int'),
      t('occupied_unexpected', 'int'),
      t('absent', 'int'),
      t('resources_with_discrepancy', 'int'),
      t('nights_compared', 'int'),
      t('coverage_bp', 'bp'),
    ],
    chart: t('records', 'int'),
    columns: () => [
      c('night', 'date'),
      c('map_name', 'text'),
      c('resource_code', 'text'),
      en('resource_type', vg('reports', 'values.sensorResourceType')),
      c('from_cm', 'cm'),
      c('to_cm', 'cm'),
      en('contractual', vg('reports', 'values.sensorContractual')),
      en('presence', vg('reports', 'values.presenceState')),
      en('physical', vg('reports', 'values.sensorPhysical')),
      { ...en('kind', vg('reports', 'values.sensorDiscrepancy')), total: 'records' },
    ],
  },
];

/** The values and their translation group of a filter in a report (report-specific values first). */
export function filterValuesOf(
  def: ReportDef,
  f: Exclude<FilterKey, 'as_of'>,
): { values: readonly string[]; group: string } {
  return def.filterValues?.[f] ?? { values: FILTER_VALUES[f], group: FILTER_VALUE_GROUP[f] };
}

/** The value of a record field; dotted keys read nested objects (maintenance `element.code`). */
export function fieldOf(record: Record<string, unknown>, key: string): unknown {
  if (!key.includes('.')) return record[key];
  let v: unknown = record;
  for (const part of key.split('.')) {
    if (v === null || typeof v !== 'object') return undefined;
    v = (v as Record<string, unknown>)[part];
  }
  return v;
}

/** Reports announced for later milestones (m7-plan §1.2): none left after M11. */
export const UPCOMING_REPORTS: readonly { key: string; milestone: string }[] = [];

export function reportDef(key: string): ReportDef | undefined {
  return REPORTS.find((r) => r.key === key);
}

/** Whether the role may open the report (spec 26.1, m7-plan D17). */
export function canOpen(def: ReportDef, can: (p: Permission) => boolean): boolean {
  if (def.permission) return can('reports.all') || can(def.permission);
  return can('reports.all') || (!def.needsAll && can('reports.operational'));
}

/** The section the records belong to: the search one when valid, else the first. */
export function sectionOf(def: ReportDef, section: string | undefined): string {
  // (the search key of the section is def.sectionParam ?? 'section'; see sectionParamOf)
  if (!def.sections) return '';
  return section && def.sections.includes(section) ? section : (def.sections[0] ?? '');
}

/** The search key of the section of a report (M10 yard: view). */
export function sectionParamOf(def: ReportDef): string {
  return def.sectionParam ?? 'section';
}

/** group_by values of a report in a section. */
export function groupByOf(def: ReportDef, section: string): readonly string[] {
  return def.groupByOf ? def.groupByOf(section) : def.groupBy;
}

/** Summary totals of a report in a section. */
export function totalsOf(def: ReportDef, section: string): readonly TotalDef[] {
  return def.totalsOf ? def.totalsOf(section) : def.totals;
}
