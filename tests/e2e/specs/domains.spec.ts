import { test, expect, type Page } from '@playwright/test';

// The UI specs drive the real Vite dev server (started by playwright.config webServer)
// and mock the backend API at the network layer, so no backend is required.
const UI_URL = process.env.HIVE_UI_URL || 'http://localhost:5180';

const SESSION = { accessToken: 'e2e-token', refreshToken: 'e2e-refresh', orgId: 'org-1' };
const ME = { id: 'user-1', email: 'e2e@example.com', displayName: 'E2E User' };
const ORG = { id: 'org-1', name: 'Hive', slug: 'hive', role: 'owner' };
const APP = { id: 'app-1', name: 'demo-app' };

interface DomainRecord {
  id: string;
  hostname: string;
  applicationId: string;
  tlsEnabled: boolean;
  routeType: string;
  pathPrefix?: string;
  stripPrefix?: boolean;
  priority?: number;
  createdAt?: string;
}

test.describe('Domains', () => {
  let domains: DomainRecord[];
  let createBodies: Record<string, unknown>[];
  let createFailure: { status: number; message: string } | null;

  async function installMocks(page: Page) {
    await page.addInitScript((session) => {
      localStorage.setItem('hive_session', JSON.stringify(session));
    }, SESSION);

    await page.route('**/api/v1/**', async (route) => {
      const req = route.request();
      const path = new URL(req.url()).pathname;
      const method = req.method();

      if (path === '/api/v1/auth/me' && method === 'GET') {
        return route.fulfill({ json: ME });
      }
      if (path === '/api/v1/applications' && method === 'GET') {
        return route.fulfill({ json: { items: [APP] } });
      }
      if (path === '/api/v1/domains') {
        if (method === 'GET') {
          return route.fulfill({ json: { items: domains } });
        }
        if (method === 'POST') {
          const body = req.postDataJSON() as Record<string, unknown>;
          createBodies.push(body);
          if (createFailure) {
            return route.fulfill({ status: createFailure.status, json: { message: createFailure.message } });
          }
          const created: DomainRecord = {
            id: `dom-${domains.length + 1}`,
            hostname: String(body.hostname),
            applicationId: String(body.applicationId),
            tlsEnabled: Boolean(body.tlsEnabled),
            routeType: String(body.routeType ?? 'host'),
            ...(body.pathPrefix ? { pathPrefix: String(body.pathPrefix) } : {}),
            ...(body.stripPrefix !== undefined ? { stripPrefix: Boolean(body.stripPrefix) } : {}),
            ...(body.priority !== undefined ? { priority: Number(body.priority) } : {}),
            createdAt: new Date().toISOString(),
          };
          domains.push(created);
          return route.fulfill({ json: created });
        }
      }
      // Generic dashboard lists loaded by AppContext.refreshAll.
      return route.fulfill({ json: { items: [] } });
    });
  }

  test.beforeEach(async ({ page }) => {
    domains = [];
    createBodies = [];
    createFailure = null;
    await installMocks(page);
    await page.goto(`${UI_URL}/dashboard/domains`);
    await expect(page.getByRole('heading', { name: 'Domains' })).toBeVisible();
  });

  async function openCreateDialog(page: Page) {
    await page.getByRole('button', { name: 'Create Domain' }).click();
    await expect(page.getByRole('heading', { name: 'Create Domain' })).toBeVisible();
  }

  test('accepts a wildcard hostname and renders the wildcard row', async ({ page }) => {
    await openCreateDialog(page);
    await page.getByLabel('Hostname', { exact: true }).fill('*.example.com');
    await page.getByLabel('Route Type').selectOption('wildcard');
    await page.getByRole('button', { name: 'Save' }).click();

    // The list refreshes after creation and shows the wildcard row.
    const row = page.getByRole('row', { name: /wildcard/ });
    await expect(row).toBeVisible();
    await expect(row).toContainText('*.example.com');

    expect(createBodies).toHaveLength(1);
    expect(createBodies[0]).toMatchObject({
      hostname: '*.example.com',
      routeType: 'wildcard',
      applicationId: APP.id,
      tlsEnabled: true,
    });
  });

  test('route type select switches path/strip field visibility', async ({ page }) => {
    await openCreateDialog(page);

    const pathPrefix = page.getByLabel('Path Prefix', { exact: true });

    // host (default): path fields hidden
    await expect(page.getByLabel('Route Type')).toHaveValue('host');
    const stripPrefix = page.getByLabel(/Strip path prefix before forwarding/);

    // path: path fields visible
    await page.getByLabel('Route Type').selectOption('path');
    await expect(pathPrefix).toBeVisible();
    await expect(stripPrefix).toBeVisible();
    await pathPrefix.fill('/api');
    await stripPrefix.check();

    // back to host: path fields hidden again
    await page.getByLabel('Route Type').selectOption('host');
    await expect(pathPrefix).toHaveCount(0);
    await expect(stripPrefix).toHaveCount(0);
  });

  test('priority input passes its value to the API', async ({ page }) => {
    await openCreateDialog(page);
    await page.getByLabel('Hostname', { exact: true }).fill('app.example.com');
    await page.getByLabel('Priority').fill('7');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect(page.getByRole('row', { name: /app\.example\.com/ })).toBeVisible();
    expect(createBodies).toHaveLength(1);
    expect(createBodies[0].priority).toBe(7);
  });

  test('invalid bare * hostname surfaces the API 400 error', async ({ page }) => {
    createFailure = { status: 400, message: 'invalid hostname: bare * is not allowed' };

    await openCreateDialog(page);
    await page.getByLabel('Hostname', { exact: true }).fill('*');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect(page.getByText('invalid hostname: bare * is not allowed')).toBeVisible();
    // The dialog stays open so the user can correct the input.
    await expect(page.getByRole('heading', { name: 'Create Domain' })).toBeVisible();
    expect(createBodies).toHaveLength(1);
    expect(createBodies[0].hostname).toBe('*');
  });
});
