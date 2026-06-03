import { test, expect } from '@playwright/test';

test.describe('Applications', () => {
  let session: { accessToken: string; refreshToken: string };
  let orgId: string;
  let projectId: string;
  let email: string;

  test.beforeAll(async ({ request }) => {
    email = `e2e-app-${Date.now()}@example.com`;
    const password = 'TestPassword123!';
    await request.post('/api/v1/auth/register', {
      data: { email, password, displayName: 'E2E App User' },
    });
    const loginRes = await request.post('/api/v1/auth/login', {
      data: { email, password },
    });
    const body = await loginRes.json();
    session = { accessToken: body.accessToken, refreshToken: body.refreshToken };

    const orgRes = await request.post('/api/v1/organizations', {
      data: { name: 'E2E App Org', slug: `e2e-app-org-${Date.now()}` },
      headers: { Authorization: `Bearer ${session.accessToken}` },
    });
    orgId = (await orgRes.json()).id;

    const projectRes = await request.post('/api/v1/projects', {
      data: { name: 'E2E App Project' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    projectId = (await projectRes.json()).id;
  });

  test('create and list application', async ({ request }) => {
    const createRes = await request.post('/api/v1/applications', {
      data: {
        projectId,
        name: 'e2e-api',
        sourceType: 'image',
        image: 'nginx:alpine',
        containerPort: 80,
      },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(createRes.ok()).toBeTruthy();
    const app = await createRes.json();
    expect(app.id).toBeDefined();
    expect(app.name).toBe('e2e-api');

    const listRes = await request.get('/api/v1/applications', {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(listRes.ok()).toBeTruthy();
    const listBody = await listRes.json();
    expect(listBody.items.some((a: { id: string }) => a.id === app.id)).toBeTruthy();

    // Cleanup
    await request.delete(`/api/v1/applications/${app.id}`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
  });
});
