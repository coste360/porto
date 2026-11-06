import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

import { OWNER } from '../support/env';
import { login } from '../support/session';

/** Fails on serious or critical axe findings (WCAG 2.x A/AA rules). */
async function expectNoSeriousViolations(page: Page, label: string) {
  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();
  const serious = result.violations.filter(
    (v) => v.impact === 'serious' || v.impact === 'critical',
  );
  const summary = serious.map(
    (v) => `${v.id} (${v.impact}): ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`,
  );
  expect(summary, `${label}: axe serious/critical violations`).toEqual([]);
}

for (const scheme of ['light', 'dark'] as const) {
  test.describe(`${scheme} theme`, () => {
    test.use({ colorScheme: scheme });

    test('login page', async ({ page }) => {
      await page.goto('/admin/login');
      await expect(page.getByRole('button', { name: 'Accedi' })).toBeVisible();
      await expectNoSeriousViolations(page, `login/${scheme}`);
    });

    test('shell and team page', async ({ page }) => {
      await login(page);
      await expectNoSeriousViolations(page, `shell/${scheme}`);
      const shell = new URL(page.url()).pathname.replace(/\/$/, '');
      await page.goto(`${shell}/team`);
      await expect(page.getByText(OWNER.email).first()).toBeVisible();
      await expectNoSeriousViolations(page, `team/${scheme}`);
    });

    test('booking page', async ({ page }) => {
      await page.goto(`/b/${OWNER.slug}`);
      await expect(page.getByRole('heading', { name: OWNER.marina })).toBeVisible();
      await expectNoSeriousViolations(page, `booking/${scheme}`);
    });
  });
}
