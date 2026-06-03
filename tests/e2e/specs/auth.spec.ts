import { test, expect } from '@playwright/test';

test.describe('Auth', () => {
  test('health endpoint is reachable', async ({ page }) => {
    const response = await page.request.get('/api/v1/health');
    expect(response.ok()).toBeTruthy();
    const body = await response.json();
    expect(body.status).toBe('ok');
  });

  test('register and login flow', async ({ page }) => {
    const email = `e2e-${Date.now()}@example.com`;
    const password = 'TestPassword123!';

    // Register
    const registerRes = await page.request.post('/api/v1/auth/register', {
      data: { email, password, displayName: 'E2E User' },
    });
    expect(registerRes.ok()).toBeTruthy();
    const registerBody = await registerRes.json();
    expect(registerBody.id).toBeDefined();

    // Login
    const loginRes = await page.request.post('/api/v1/auth/login', {
      data: { email, password },
    });
    expect(loginRes.ok()).toBeTruthy();
    const loginBody = await loginRes.json();
    expect(loginBody.accessToken).toBeDefined();
    expect(loginBody.refreshToken).toBeDefined();
  });
});
