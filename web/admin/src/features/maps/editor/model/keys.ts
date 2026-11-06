import type { EditorDocument } from './document';

let counter = 0;

/**
 * A new client key ("n:" + counter), unique in the session and not used by the document (a restored draft
 * may already hold keys).
 */
export function newKey(doc?: EditorDocument): string {
  for (;;) {
    counter++;
    const key = `n:${counter}`;
    if (!doc?.elements.has(key)) return key;
  }
}

/** Prefix of the keys of one generator run ("g<n>-"). */
let generatorRuns = 0;
export function newGeneratorPrefix(doc?: EditorDocument): string {
  for (;;) {
    generatorRuns++;
    const prefix = `g${generatorRuns}-`;
    if (!doc || ![...doc.elements.keys()].some((k) => k.startsWith(prefix))) return prefix;
  }
}
