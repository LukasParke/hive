import { test, expect } from '@playwright/test';

test.describe('Organizations', () => {
  let session: { accessToken: string; refreshToken: string };
  let email: string;

  test.beforeAll(async ({ request }) => {
    email = `e2e-org-${Date.now()}@example.com`;
    const password = 'TestPassword123!';
    await request.post('/api/v1/auth/register', {
      data: { email, password, displayName: 'E2E Org User' },
    });
    const loginRes = await request.post('/api/v1/auth/login', {
      data: { email, password },
    });
    const body = await loginRes.json();
    session = { accessToken: body.accessToken, refreshToken: body.refreshToken };
  });

  test('create and list organizations', async ({ request }) => {
    const createRes = await request.post('/api/v1/organizations', {
      data: { name: 'E2E Org', slug: `e2e-org-${Date.now()}` },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
      },
    });
    expect(createRes.ok()).toBeTruthy();
    const createBody = await createRes.json();
    expect(createBody.id).toBeDefined();

    const listRes = await request.get('/api/v1/organizations', {
      headers: { Authorization: `Bearer ${session.accessToken}` },
    });
    expect(listRes.ok()).toBeTruthy();
    const listBody = await listRes.json();
    expect(Array.isArray(listBody.items)).toBeTruthy();
    expect(listBody.items.length).toBeGreaterThan(0);
  });
});
