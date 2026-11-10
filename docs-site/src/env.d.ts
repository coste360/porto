/// <reference path="../.astro/types.d.ts" />

// Starlight's UI strings extended with Porto's own keys (`porto.*`), taken from
// the Italian source file, so Astro.locals.t('porto.…') is type-checked.
declare namespace StarlightApp {
  type PortoStrings = typeof import('./content/i18n/it.json');
  interface I18n extends PortoStrings {}
}
