import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// jsdom lacks the pointer capture and scrolling APIs that Radix Select calls.
const proto = window.HTMLElement.prototype as unknown as Record<string, unknown>;
proto.hasPointerCapture ??= () => false;
proto.setPointerCapture ??= () => undefined;
proto.releasePointerCapture ??= () => undefined;
proto.scrollIntoView ??= () => undefined;

/** Opens the Radix Select labelled `label` (inside `container` when given) and picks `option`. */
export async function pickOption(
  label: string | RegExp,
  option: string | RegExp,
  container?: HTMLElement,
) {
  const scope = container ? within(container) : screen;
  await userEvent.click(scope.getByRole('combobox', { name: label }));
  await userEvent.click(await screen.findByRole('option', { name: option }));
}
