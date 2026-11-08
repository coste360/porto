// Pure state of the guided yard quote (m10-plan §4.2): the five steps, the inputs of each step and the request
// sent to the stateless preview and to the create call. Quantities, prices and amounts stay integers (milli and
// cents); nothing here formats or calls the API.
import type {
  FormulaValue,
  YardBillingMode,
  YardLineOverride,
  YardQuoteInput,
  YardQuoteLine,
} from '@porto/shared/api';
import { formatQtyMilli } from '@porto/shared/lib/format';

export const WIZARD_STEPS = ['vessel', 'catalog', 'variables', 'summary', 'conditions'] as const;
export type WizardStep = (typeof WIZARD_STEPS)[number];

export type Selection = { package_id?: string; item_id?: string };

export type WizardState = {
  step: WizardStep;
  customerId: string;
  vesselId: string;
  selections: Selection[];
  jobInputs: Record<string, FormulaValue>;
  vesselOverrides: Record<string, FormulaValue>;
  overrides: YardLineOverride[];
  billingMode: YardBillingMode;
  depositBp: number | null;
  validUntil: string;
  plannedStartOn: string;
  plannedDeliveryOn: string;
  conditions: string;
  notes: string;
};

export function initialWizard(): WizardState {
  return {
    step: 'vessel',
    customerId: '',
    vesselId: '',
    selections: [],
    jobInputs: {},
    vesselOverrides: {},
    overrides: [],
    billingMode: 'actual',
    depositBp: null,
    validUntil: '',
    plannedStartOn: '',
    plannedDeliveryOn: '',
    conditions: '',
    notes: '',
  };
}

export type OverrideField = 'qty_milli' | 'unit_cents' | 'discount_cents';

export type WizardAction =
  | { type: 'goto'; step: WizardStep }
  | { type: 'next' }
  | { type: 'back' }
  | { type: 'customer'; id: string }
  | { type: 'vessel'; id: string }
  | { type: 'toggle'; selection: Selection }
  | { type: 'jobInput'; name: string; value: FormulaValue | null }
  | { type: 'vesselOverride'; name: string; value: FormulaValue | null }
  | { type: 'override'; seq: number; field: OverrideField; value: number | null }
  | { type: 'resetLine'; seq: number }
  | { type: 'terms'; patch: Partial<Omit<WizardState, 'step' | 'selections' | 'overrides'>> };

function sameSelection(a: Selection, b: Selection): boolean {
  return (a.package_id ?? '') === (b.package_id ?? '') && (a.item_id ?? '') === (b.item_id ?? '');
}

function setOrDelete(
  map: Record<string, FormulaValue>,
  name: string,
  value: FormulaValue | null,
): Record<string, FormulaValue> {
  const out = { ...map };
  if (value === null) delete out[name];
  else out[name] = value;
  return out;
}

/** The step reducer. Changing the selection drops the overrides: line numbers change with the lines. */
export function wizardReducer(state: WizardState, action: WizardAction): WizardState {
  const index = WIZARD_STEPS.indexOf(state.step);
  switch (action.type) {
    case 'goto':
      return WIZARD_STEPS.indexOf(action.step) <= index || canAdvance(state)
        ? { ...state, step: action.step }
        : state;
    case 'next':
      return canAdvance(state) && index < WIZARD_STEPS.length - 1
        ? { ...state, step: WIZARD_STEPS[index + 1]! }
        : state;
    case 'back':
      return index > 0 ? { ...state, step: WIZARD_STEPS[index - 1]! } : state;
    case 'customer':
      return { ...state, customerId: action.id };
    case 'vessel':
      return { ...state, vesselId: action.id, vesselOverrides: {} };
    case 'toggle': {
      const has = state.selections.some((s) => sameSelection(s, action.selection));
      return {
        ...state,
        selections: has
          ? state.selections.filter((s) => !sameSelection(s, action.selection))
          : [...state.selections, action.selection],
        overrides: [],
      };
    }
    case 'jobInput':
      return { ...state, jobInputs: setOrDelete(state.jobInputs, action.name, action.value) };
    case 'vesselOverride':
      return {
        ...state,
        vesselOverrides: setOrDelete(state.vesselOverrides, action.name, action.value),
      };
    case 'override': {
      const current = state.overrides.find((o) => o.seq === action.seq) ?? { seq: action.seq };
      const next: YardLineOverride = { ...current };
      if (action.value === null) delete next[action.field];
      else next[action.field] = action.value;
      const rest = state.overrides.filter((o) => o.seq !== action.seq);
      const empty = Object.keys(next).length === 1;
      return { ...state, overrides: empty ? rest : [...rest, next].sort((a, b) => a.seq - b.seq) };
    }
    case 'resetLine':
      return { ...state, overrides: state.overrides.filter((o) => o.seq !== action.seq) };
    case 'terms':
      return { ...state, ...action.patch };
  }
}

/** Whether the current step is complete enough to go on. */
export function canAdvance(state: WizardState): boolean {
  switch (state.step) {
    case 'vessel':
      return state.customerId !== '' && state.vesselId !== '';
    case 'catalog':
      return state.selections.length > 0;
    default:
      return true;
  }
}

/** The request of the preview and of the create call (empty optional fields are left out). */
export function toQuoteInput(state: WizardState): YardQuoteInput {
  const input: YardQuoteInput = {
    customer_id: state.customerId,
    vessel_id: state.vesselId,
    selections: state.selections,
    billing_mode: state.billingMode,
  };
  if (Object.keys(state.jobInputs).length > 0) input.job_inputs = state.jobInputs;
  if (Object.keys(state.vesselOverrides).length > 0) input.vessel_overrides = state.vesselOverrides;
  if (state.overrides.length > 0) input.overrides = state.overrides;
  if (state.depositBp !== null) input.deposit_bp = state.depositBp;
  if (state.validUntil) input.valid_until = state.validUntil;
  if (state.plannedStartOn) input.planned_start_on = state.plannedStartOn;
  if (state.plannedDeliveryOn) input.planned_delivery_on = state.plannedDeliveryOn;
  if (state.conditions.trim()) input.conditions = state.conditions.trim();
  if (state.notes.trim()) input.notes = state.notes.trim();
  return input;
}

/** Whether a line carries the "manuale" badge (overridden quantity, price or discount, or a manual line). */
export function isManual(
  line: Pick<YardQuoteLine, 'manual' | 'qty_overridden' | 'price_overridden'>,
): boolean {
  return line.manual || line.qty_overridden || line.price_overridden;
}

/** A formula value as shown in a field: numbers in milli become decimals ("2,5"). */
export function valueText(value: FormulaValue | undefined, locale = 'it'): string {
  if (value === undefined) return '';
  if (typeof value === 'number') return formatQtyMilli(value, locale);
  return String(value);
}
