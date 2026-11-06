// Live validation (m1-plan §5.3, §11 D4): the server's layout.ValidateMap runs on the unsaved document through
// POST /validate, debounced; a newer request always wins over an older answer.
import { useEffect, useMemo, useRef, useState } from 'react';

import { useApi, validateMap, type MapIssue, type MapValidationReport } from '@porto/shared/api';

import { toSaveRequest, type EditorDocument } from './document';

export const VALIDATION_DEBOUNCE_MS = 800;

export type ValidationStatus = 'idle' | 'pending' | 'ok' | 'error';

/** Issues indexed by every ref they name. */
export function issuesByRef(report: MapValidationReport | null): Map<string, MapIssue[]> {
  const out = new Map<string, MapIssue[]>();
  if (!report) return out;
  for (const issue of [...report.errors, ...report.warnings]) {
    for (const ref of issue.refs) {
      const list = out.get(ref);
      if (list) list.push(issue);
      else out.set(ref, [issue]);
    }
  }
  return out;
}

/** Issues grouped by code, errors first (the validation panel). */
export function groupIssues(
  report: MapValidationReport | null,
): { code: string; severity: MapIssue['severity']; issues: MapIssue[] }[] {
  if (!report) return [];
  const groups = new Map<
    string,
    { code: string; severity: MapIssue['severity']; issues: MapIssue[] }
  >();
  for (const issue of [...report.errors, ...report.warnings]) {
    const key = `${issue.severity}:${issue.code}`;
    const g = groups.get(key);
    if (g) g.issues.push(issue);
    else groups.set(key, { code: issue.code, severity: issue.severity, issues: [issue] });
  }
  return [...groups.values()].sort((a, b) =>
    a.severity === b.severity ? a.code.localeCompare(b.code) : a.severity === 'error' ? -1 : 1,
  );
}

export function useLiveValidation(opts: {
  marinaId: string;
  mapId: string;
  doc: EditorDocument | null;
  enabled: boolean;
  /** A report received from elsewhere (422 on save) replaces the live one. */
  override?: MapValidationReport | null;
}) {
  const api = useApi();
  const [report, setReport] = useState<MapValidationReport | null>(null);
  const [status, setStatus] = useState<ValidationStatus>('idle');
  const latest = useRef(0);
  const { marinaId, mapId, doc, enabled } = opts;

  useEffect(() => {
    if (!enabled || !doc) return;
    const id = ++latest.current;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      setStatus('pending');
      validateMap(api, marinaId, mapId, toSaveRequest(doc), controller.signal)
        .then((r) => {
          if (id !== latest.current) return; // stale answer
          setReport(r);
          setStatus('ok');
        })
        .catch(() => {
          if (id !== latest.current) return;
          setStatus('error');
        });
    }, VALIDATION_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [api, marinaId, mapId, doc, enabled]);

  const effective = opts.override ?? report;
  const byRef = useMemo(() => issuesByRef(effective), [effective]);
  return { report: effective, status, byRef };
}
