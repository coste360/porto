import { defineCollection } from 'astro:content';
import { z } from 'astro/zod';
import { docsLoader, i18nLoader } from '@astrojs/starlight/loaders';
import { docsSchema, i18nSchema } from '@astrojs/starlight/schema';
import italian from './content/i18n/it.json';

/**
 * UI strings of Porto's own components (`porto.*` keys). The Italian file is
 * the source: its keys define the schema, so `Astro.locals.t('porto.…')` is
 * type-checked and the English file is validated against the same keys.
 */
const portoStrings = z.object(
  Object.fromEntries(
    Object.keys(italian)
      .filter((key) => key.startsWith('porto.'))
      .map((key) => [key, z.string().optional()]),
  ),
);

/** Product hero of the home pages, rendered by the Hero override. */
const home = z
  .object({
    /** One line of key facts under the title. */
    facts: z.string(),
    /** Screenshot names in src/assets/screenshots/ (without extension). */
    shot: z.object({ light: z.string(), dark: z.string(), alt: z.string() }),
    phone: z.object({ name: z.string(), alt: z.string() }).optional(),
    techLink: z.object({ text: z.string(), link: z.string() }),
  })
  .optional();

export const collections = {
  docs: defineCollection({ loader: docsLoader(), schema: docsSchema({ extend: z.object({ home }) }) }),
  i18n: defineCollection({ loader: i18nLoader(), schema: i18nSchema({ extend: portoStrings }) }),
};
