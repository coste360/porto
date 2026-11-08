/**
 * Leaves the page for the provider's hosted checkout. In embed mode the top window navigates (hosted checkouts
 * refuse framing, m8-plan §16 D18); the navigation follows the customer's click.
 */
export function goToCheckout(url: string, embedded: boolean, win: Window = window): void {
  if (embedded) win.open(url, '_top');
  else win.location.assign(url);
}
