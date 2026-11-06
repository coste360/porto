import { describe, expect, it } from 'vitest';

import { detectLocale, initI18n } from './init';

describe('i18n', () => {
  it('uses Italian as source and fallback', () => {
    const i18n = initI18n();
    expect(i18n.language).toBe('it');
    expect(i18n.t('common:actions.save')).toBe('Salva');
  });

  it('translates to English', () => {
    const i18n = initI18n({ lng: 'en' });
    expect(i18n.t('common:actions.save')).toBe('Save');
  });

  it('detects the locale from an explicit value, then the browser', () => {
    expect(detectLocale('en')).toBe('en');
    expect(detectLocale(null, ['en-GB', 'it'])).toBe('en');
    expect(detectLocale('de', ['fr-FR'])).toBe('it');
    expect(detectLocale(null, ['it-IT', 'en'])).toBe('it');
  });

  it('defaults to Italian for browsers whose preferred language is neither it nor en', () => {
    expect(detectLocale(null, ['de-DE', 'de', 'en-US', 'en'])).toBe('it');
    expect(detectLocale(null, ['fr-FR'])).toBe('it');
    expect(detectLocale(null, [])).toBe('it');
    expect(detectLocale(null, ['EN-us'])).toBe('en');
  });
});
