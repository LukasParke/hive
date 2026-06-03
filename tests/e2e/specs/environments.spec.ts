import { test, expect } from '@playwright/test';

test.describe('Environments', () => {
  let session: { accessToken: string; refreshToken: string };
  let orgId: string;
  let projectId: string;
  let email: string;

  test.beforeAll(async ({ request }) => {
    email = `e2e-env-${Date.now()}@example.com`;
    const password = 'TestPassword123!';
    await request.post('/api/v1/auth/register', {
      data: { email, password, displayName: 'E2E Env User' },
    });
    const loginRes = await request.post('/api/v1/auth/login', {
      data: { email, password },
    });
    const body = await loginRes.json();
    session = { accessToken: body.accessToken, refreshToken: body.refreshToken };

    const orgRes = await request.post('/api/v1/organizations', {
      data: { name: 'E2E Env Org', slug: `e2e-env-org-${Date.now()}` },
      headers: { Authorization: `Bearer ${session.accessToken}` },
    });
    orgId = (await orgRes.json()).id;

    const projectRes = await request.post('/api/v1/projects', {
      data: { name: 'E2E Env Project' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    projectId = (await projectRes.json()).id;
  });

  test('CRUD environment', async ({ request }) => {
    const createRes = await request.post('/api/v1/environments', {
      data: { projectId, name: 'staging', slug: 'stg' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(createRes.ok()).toBeTruthy();
    const env = await createRes.json();
    expect(env.id).toBeDefined();

    const listRes = await request.get('/api/v1/environments', {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(listRes.ok()).toBeTruthy();
    const listBody = await listRes.json();
    expect(listBody.items.some((e: { id: string }) => e.id === env.id)).toBeTruthy();

    const updateRes = await request.put(`/api/v1/environments/${env.id}`, {
      data: { name: 'production', slug: 'prod' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(updateRes.ok()).toBeTruthy();

    const deleteRes = await request.delete(`/api/v1/environments/${env.id}`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(deleteRes.ok()).toBeTruthy();
  });
});
