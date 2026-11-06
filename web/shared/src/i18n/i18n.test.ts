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
  });
});
