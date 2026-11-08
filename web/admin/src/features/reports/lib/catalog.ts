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
  | 'as_of';

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
];

/** Reports announced for later milestones (m7-plan §1.2). */
export const UPCOMING_REPORTS = [
  { key: 'yard', milestone: 'M10' },
  { key: 'rental', milestone: 'M11' },
] as const;

export function reportDef(key: string): ReportDef | undefined {
  return REPORTS.find((r) => r.key === key);
}

/** Whether the role may open the report (spec 26.1, m7-plan D17). */
export function canOpen(def: ReportDef, can: (p: Permission) => boolean): boolean {
  return can('reports.all') || (!def.needsAll && can('reports.operational'));
}

/** The section the records belong to: the search one when valid, else the first. */
export function sectionOf(def: ReportDef, section: string | undefined): string {
  if (!def.sections) return '';
  return section && def.sections.includes(section) ? section : (def.sections[0] ?? '');
}
