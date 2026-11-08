/** Picks the text of the current language from an { it, en } object (Italian is the source). */
export function localized(text: { it?: string; en?: string } | undefined, lang: string): string {
  if (!text) return '';
  if (lang.startsWith('en') && text.en) return text.en;
  return text.it ?? text.en ?? '';
}
