// The publicProfile namespace is not in the booking entry (m12-plan §12.3: the profile code and texts load with
// the lazy chunk). registerProfileTexts adds it to the running i18next instance, once.
import type { i18n as I18n } from 'i18next';

import enPublicProfile from '../../../shared/src/i18n/locales/en/publicProfile.json';
import itPublicProfile from '../../../shared/src/i18n/locales/it/publicProfile.json';

export const PROFILE_NS = 'publicProfile';

export function registerProfileTexts(i18n: I18n): void {
  if (!i18n.hasResourceBundle('it', PROFILE_NS))
    i18n.addResourceBundle('it', PROFILE_NS, itPublicProfile, true, true);
  if (!i18n.hasResourceBundle('en', PROFILE_NS))
    i18n.addResourceBundle('en', PROFILE_NS, enPublicProfile, true, true);
}
