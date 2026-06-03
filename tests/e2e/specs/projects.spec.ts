import { test, expect } from '@playwright/test';

test.describe('Projects', () => {
  let session: { accessToken: string; refreshToken: string };
  let orgId: string;
  let email: string;

  test.beforeAll(async ({ request }) => {
    email = `e2e-project-${Date.now()}@example.com`;
    const password = 'TestPassword123!';
    await request.post('/api/v1/auth/register', {
      data: { email, password, displayName: 'E2E Project User' },
    });
    const loginRes = await request.post('/api/v1/auth/login', {
      data: { email, password },
    });
    const body = await loginRes.json();
    session = { accessToken: body.accessToken, refreshToken: body.refreshToken };

    const orgRes = await request.post('/api/v1/organizations', {
      data: { name: 'E2E Project Org', slug: `e2e-project-org-${Date.now()}` },
      headers: { Authorization: `Bearer ${session.accessToken}` },
    });
    const orgBody = await orgRes.json();
    orgId = orgBody.id;
  });

  test('CRUD project', async ({ request }) => {
    // Create
    const createRes = await request.post('/api/v1/projects', {
      data: { name: 'E2E Project' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(createRes.ok()).toBeTruthy();
    const project = await createRes.json();
    expect(project.id).toBeDefined();
    expect(project.name).toBe('E2E Project');

    // List
    const listRes = await request.get('/api/v1/projects', {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(listRes.ok()).toBeTruthy();
    const listBody = await listRes.json();
    expect(listBody.items.some((p: { id: string }) => p.id === project.id)).toBeTruthy();

    // Update
    const updateRes = await request.put(`/api/v1/projects/${project.id}`, {
      data: { name: 'E2E Project Updated' },
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(updateRes.ok()).toBeTruthy();

    // Get
    const getRes = await request.get(`/api/v1/projects/${project.id}`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(getRes.ok()).toBeTruthy();
    const getBody = await getRes.json();
    expect(getBody.name).toBe('E2E Project Updated');

    // Delete
    const deleteRes = await request.delete(`/api/v1/projects/${project.id}`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'X-Organization-Id': orgId,
      },
    });
    expect(deleteRes.ok()).toBeTruthy();
  });
});
