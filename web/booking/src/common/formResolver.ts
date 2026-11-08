// A react-hook-form resolver over a zod schema. The booking workspace cannot resolve @hookform/resolvers (it is
// installed under shared/ and admin/ only), so the public pages use this small adapter: the first issue of each
// field becomes its error, with the issue message as the i18n key.
import type { FieldErrors, FieldValues, Resolver } from 'react-hook-form';
import type { z } from 'zod';

export function zodFormResolver<In extends FieldValues, Out extends FieldValues>(
  schema: z.ZodType<Out, In>,
): Resolver<In, unknown, Out> {
  return async (values) => {
    const r = await schema.safeParseAsync(values);
    if (r.success) return { values: r.data, errors: {} };
    const errors: Record<string, unknown> = {};
    for (const issue of r.error.issues) {
      const path = issue.path.map(String);
      if (path.length === 0) continue;
      let node = errors;
      for (const seg of path.slice(0, -1)) {
        node[seg] = (node[seg] as Record<string, unknown> | undefined) ?? {};
        node = node[seg] as Record<string, unknown>;
      }
      const last = path[path.length - 1] as string;
      if (!node[last]) node[last] = { type: issue.code, message: issue.message };
    }
    return { values: {}, errors: errors as FieldErrors<In> };
  };
}
