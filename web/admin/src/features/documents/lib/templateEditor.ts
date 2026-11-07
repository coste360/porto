// Pure helpers of the templates editor (spec 15.1–15.3, m4-plan §6.1, §8.1): the field catalogue as a tree,
// token insertion at the cursor, `each`/`if` snippets and the inline error markers from a `template_invalid`
// problem (details [{line, col, code, path?}]). The language is evaluated only by the server.
import { isProblem, type Schemas } from '@porto/shared/api';

type FieldDef = Schemas['FieldDef'];

export type FieldNode = {
  /** Last segment of the path ("name" of "customer.name"). */
  name: string;
  /** Full path ("customer.name"); for children of a list, relative to `this`. */
  path: string;
  field?: FieldDef;
  children: FieldNode[];
};

/**
 * Builds the catalogue tree from the flat field list (paths with dots; list items are `list.field`). Objects that
 * are only containers have no `field`. Children are sorted by name; the order of the roots follows the catalogue.
 */
export function fieldTree(fields: readonly FieldDef[]): FieldNode[] {
  const roots: FieldNode[] = [];
  const index = new Map<string, FieldNode>();
  const ensure = (path: string): FieldNode => {
    const found = index.get(path);
    if (found) return found;
    const dot = path.lastIndexOf('.');
    const node: FieldNode = { name: path.slice(dot + 1), path, children: [] };
    index.set(path, node);
    if (dot < 0) roots.push(node);
    else ensure(path.slice(0, dot)).children.push(node);
    return node;
  };
  for (const f of fields) ensure(f.path).field = f;
  const sort = (nodes: FieldNode[]) => {
    nodes.sort((a, b) => a.name.localeCompare(b.name));
    nodes.forEach((n) => sort(n.children));
  };
  roots.forEach((r) => sort(r.children));
  return roots;
}

/** The nearest list ancestor of a path, if any ("installments" for "installments.amount"). */
export function listAncestor(fields: readonly FieldDef[], path: string): FieldDef | undefined {
  const lists = fields.filter((f) => f.list && path.startsWith(`${f.path}.`));
  return lists.sort((a, b) => b.path.length - a.path.length)[0];
}

const FILTER_BY_TYPE: Partial<Record<Schemas['FieldType'], string>> = {
  cents: 'money',
  cm: 'meters',
  bp: 'bp',
  millis: 'millis',
  date: 'date',
};

/**
 * The token inserted for a field: `{{path | filter}}` with the default filter of its type; inside an `each` of
 * the field's list ancestor the path is relative (`this.x`). Lists insert a whole `each` block.
 */
export function tokenFor(field: FieldDef, insideList?: string): string {
  if (field.list) return eachSnippet(field.path);
  const path =
    insideList && field.path.startsWith(`${insideList}.`)
      ? `this.${field.path.slice(insideList.length + 1)}`
      : field.path;
  const filter = FILTER_BY_TYPE[field.type];
  return filter ? `{{${path} | ${filter}}}` : `{{${path}}}`;
}

export function eachSnippet(path: string): string {
  return `{{#each ${path}}}\n- \n{{/each}}`;
}

export function ifSnippet(path: string): string {
  return `{{#if ${path}}}\n\n{{else}}\n\n{{/if}}`;
}

/** Inserts text at the selection of a textarea value; returns the new value and the caret after the text. */
export function insertAt(
  value: string,
  selectionStart: number,
  selectionEnd: number,
  text: string,
): { value: string; caret: number } {
  const start = Math.max(0, Math.min(selectionStart, value.length));
  const end = Math.max(start, Math.min(selectionEnd, value.length));
  return { value: value.slice(0, start) + text + value.slice(end), caret: start + text.length };
}

/** Which `{{#each}}` list the caret is in, by scanning the open/close blocks before it (innermost wins). */
export function enclosingEach(value: string, caret: number): string | undefined {
  const before = value.slice(0, caret);
  const stack: string[] = [];
  const re = /\{\{\s*(#each\s+([\w.]+)|\/each)\s*\}\}/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(before)) !== null) {
    if (m[2]) stack.push(m[2]);
    else stack.pop();
  }
  return stack[stack.length - 1];
}

export type TemplateIssue = {
  line: number;
  col: number;
  code: string;
  path?: string;
  /** Which part of the template (body, header, footer); body when the server does not say. */
  part: 'body' | 'header' | 'footer';
};

/** Errors by line of a `template_invalid` / `template_limits` problem, or [] for other errors. */
export function templateIssues(error: unknown): TemplateIssue[] {
  if (!isProblem(error)) return [];
  if (error.type !== 'template_invalid' && error.type !== 'template_limits') return [];
  // The issues arrive as `details` (an array) or `details.issues`; ProblemError keeps only object details, so the
  // server is asked to wrap them (`{"issues": [...]}`); both shapes are read.
  const raw = error.details as unknown;
  const wrapped =
    raw && typeof raw === 'object' && !Array.isArray(raw)
      ? ((raw as { issues?: unknown; errors?: unknown }).issues ??
        (raw as { errors?: unknown }).errors)
      : undefined;
  const list: unknown[] = Array.isArray(raw) ? raw : Array.isArray(wrapped) ? wrapped : [];
  const out: TemplateIssue[] = [];
  for (const item of list) {
    if (!item || typeof item !== 'object') continue;
    const r = item as Record<string, unknown>;
    const line = typeof r.line === 'number' ? r.line : 0;
    const col = typeof r.col === 'number' ? r.col : 0;
    const part = r.part === 'header' || r.part === 'footer' ? r.part : 'body';
    out.push({
      line,
      col,
      code: typeof r.code === 'string' ? r.code : 'invalid',
      ...(typeof r.path === 'string' ? { path: r.path } : {}),
      part,
    });
  }
  if (out.length === 0) out.push({ line: 0, col: 0, code: error.type, part: 'body' });
  return out.sort((a, b) => a.line - b.line || a.col - b.col);
}

/** Lines of a text with their 1-based numbers and the issues on each line (gutter of the editor). */
export function markedLines(text: string, issues: readonly TemplateIssue[]) {
  return text.split('\n').map((content, i) => ({
    number: i + 1,
    content,
    issues: issues.filter((x) => x.line === i + 1),
  }));
}

/** Character offset of (line, col) (1-based) in a text, to move the caret to an error. */
export function offsetOf(text: string, line: number, col: number): number {
  const lines = text.split('\n');
  let off = 0;
  for (let i = 0; i < Math.min(line - 1, lines.length); i++) off += lines[i]!.length + 1;
  return Math.min(off + Math.max(0, col - 1), text.length);
}

// ---- preview blob ----

/** Holds one object URL at a time for the preview iframe and revokes the previous one. */
export class PreviewUrl {
  private current: string | null = null;
  constructor(
    private readonly create: (b: Blob) => string = (b) => URL.createObjectURL(b),
    private readonly revoke: (u: string) => void = (u) => URL.revokeObjectURL(u),
  ) {}

  set(blob: Blob): string {
    const next = this.create(
      blob.type === 'application/pdf' ? blob : new Blob([blob], { type: 'application/pdf' }),
    );
    this.clear();
    this.current = next;
    return next;
  }

  get url(): string | null {
    return this.current;
  }

  clear(): void {
    if (this.current) this.revoke(this.current);
    this.current = null;
  }
}
