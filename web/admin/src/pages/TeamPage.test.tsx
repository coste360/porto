import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { Member } from '@porto/shared/api';
import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../test/renderApp';
import { addMemberSchema } from '../lib/schemas';

const members: Member[] = [
  {
    user_id: 'u1',
    email: 'owner@porto.test',
    name: 'Owner Prova',
    role: 'owner',
    scope: 'organization',
    active: true,
    totp_enabled: false,
    invitation_pending: false,
    last_login_at: null,
  },
  {
    user_id: 'u2',
    email: 'ufficio@porto.test',
    name: 'Ufficio Prova',
    role: 'office',
    scope: 'marina',
    active: true,
    totp_enabled: false,
    invitation_pending: true,
    last_login_at: null,
  },
];

const membersPath = `/api/admin/v1/marinas/${MARINA_ID}/members`;

describe('TeamPage', () => {
  it('lists the members', async () => {
    renderApp(`/m/${MARINA_ID}/team`, [
      meRoute(makeMe()),
      { path: membersPath, body: { items: members } },
    ]);
    expect(await screen.findByText('Ufficio Prova')).toBeInTheDocument();
    expect(screen.getByText('ufficio@porto.test')).toBeInTheDocument();
    expect(screen.getByText('Invito in attesa')).toBeInTheDocument();
  });

  it('validates and adds a member with an Idempotency-Key', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/team`, [
      meRoute(makeMe()),
      { path: membersPath, body: { items: members } },
      { method: 'POST', path: membersPath, status: 201, body: members[1] },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Aggiungi persona' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi' }));
    expect(await within(dialog).findAllByText('Campo obbligatorio.')).not.toHaveLength(0);
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await userEvent.type(within(dialog).getByLabelText('Nome'), 'Nuova Persona');
    await userEvent.type(within(dialog).getByLabelText('Email'), 'nuova@porto.test');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.body).toEqual({
      name: 'Nuova Persona',
      email: 'nuova@porto.test',
      role: 'office',
      locale: 'it',
    });
    expect(post?.headers.get('Idempotency-Key')).toBeTruthy();
    expect(post?.headers.get('X-CSRF-Token')).toBe('csrf-test');
  });

  it('shows already_member from the server', async () => {
    renderApp(`/m/${MARINA_ID}/team`, [
      meRoute(makeMe()),
      { path: membersPath, body: { items: members } },
      { method: 'POST', path: membersPath, status: 409, body: problem('already_member', 409) },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Aggiungi persona' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Nome'), 'Ufficio Prova');
    await userEvent.type(within(dialog).getByLabelText('Email'), 'ufficio@porto.test');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi' }));
    expect(
      await within(dialog).findByText('Questa persona fa già parte del team.'),
    ).toBeInTheDocument();
  });

  it('rejects an invalid email in the schema', () => {
    const result = addMemberSchema.safeParse({ name: 'X', email: 'not-an-email', role: 'office' });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.message).toBe('common:validation.email');
  });
});
