import { test, expect, type Page } from '@playwright/test';

// The UI specs drive the real Vite dev server (started by playwright.config webServer)
// and mock the backend API at the network layer, so no backend is required.
const UI_URL = process.env.HIVE_UI_URL || 'http://localhost:5180';
const SESSION = { accessToken: 'e2e-token', refreshToken: 'e2e-refresh', orgId: 'org-1' };
const ME = { id: 'user-1', email: 'e2e@example.com', displayName: 'E2E User' };

interface IngressRule {
  hostname: string;
  service?: string;
}

interface TunnelRecord {
  id: string;
  name: string;
  cloudflareTunnelId?: string;
  status: 'creating' | 'deployed' | 'error' | 'deleting';
  ingress: IngressRule[];
  connector?: { runningReplicas: number; desiredReplicas: number; cloudflareStatus?: string };
  errorMessage?: string;
  createdAt?: string;
}

function makeTunnel(overrides: Partial<TunnelRecord> = {}): TunnelRecord {
  return {
    id: `tun-${Math.random().toString(36).slice(2, 8)}`,
    name: 'seed-tunnel',
    status: 'deployed',
    ingress: [{ hostname: 'app.example.com', service: 'http://traefik:80' }],
    createdAt: new Date().toISOString(),
    ...overrides,
  };
}

test.describe('Tunnels', () => {
  let tunnels: TunnelRecord[];
  let createBodies: Record<string, unknown>[];
  let updateIngressBodies: { id: string; body: Record<string, unknown> }[];
  let deleteIds: string[];
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
      if (path === '/api/v1/tunnels') {
        if (method === 'GET') {
          return route.fulfill({ json: { items: tunnels } });
        }
        if (method === 'POST') {
          const body = req.postDataJSON() as Record<string, unknown>;
          createBodies.push(body);
          if (createFailure) {
            return route.fulfill({ status: createFailure.status, json: { message: createFailure.message } });
          }
          const created: TunnelRecord = {
            ...makeTunnel(),
            id: `tun-${tunnels.length + 1}`,
            name: String(body.name),
            cloudflareTunnelId: 'cf-tunnel-1',
            status: 'deployed',
            ingress: body.ingress as IngressRule[],
            connector: { runningReplicas: 1, desiredReplicas: 1, cloudflareStatus: 'connected' },
          };
          tunnels.push(created);
          return route.fulfill({ json: created });
        }
      }
      const ingressMatch = path.match(/^\/api\/v1\/tunnels\/([^/]+)\/ingress$/);
      if (ingressMatch && method === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        updateIngressBodies.push({ id: ingressMatch[1], body });
        const tunnel = tunnels.find((t) => t.id === ingressMatch[1]);
        if (tunnel) tunnel.ingress = body.ingress as IngressRule[];
        return route.fulfill({ json: tunnel });
      }
      const tunnelMatch = path.match(/^\/api\/v1\/tunnels\/([^/]+)$/);
      if (tunnelMatch && method === 'DELETE') {
        deleteIds.push(tunnelMatch[1]);
        tunnels = tunnels.filter((t) => t.id !== tunnelMatch[1]);
        return route.fulfill({ json: { status: 'ok' } });
      }
      // Generic dashboard lists loaded by AppContext.refreshAll.
      return route.fulfill({ json: { items: [] } });
    });
  }

  test.beforeEach(async ({ page }) => {
    tunnels = [];
    createBodies = [];
    updateIngressBodies = [];
    deleteIds = [];
    createFailure = null;
    await installMocks(page);
    await page.goto(`${UI_URL}/dashboard/tunnels`);
    await expect(page.getByRole('heading', { name: 'Cloudflare Tunnels' })).toBeVisible();
  });

  async function openCreateDialog(page: Page) {
    await page.getByRole('button', { name: 'Create Tunnel' }).click();
    await expect(page.getByRole('heading', { name: 'Create Cloudflare Tunnel' })).toBeVisible();
  }

  test('create flow with two ingress rows shows the deployed row with connector health', async ({ page }) => {
    await openCreateDialog(page);
    await page.getByLabel('Name', { exact: true }).fill('prod-edge');
    await page.getByLabel('Account ID', { exact: true }).fill('acc-123');
    await page.getByLabel(/Zone ID \(optional/).fill('zone-123');
    await page.getByLabel('API Token').fill('tok-abc');

    const hostnameInputs = page.getByPlaceholder('app.example.com or *.example.com');
    await hostnameInputs.first().fill('app.example.com');

    await page.getByRole('button', { name: '+ Add rule' }).click();
    await hostnameInputs.nth(1).fill('*.example.com');
    const serviceInputs = page.getByPlaceholder('http://traefik:80');
    await expect(serviceInputs.nth(1)).toHaveValue('http://traefik:80');

    await page.getByRole('button', { name: 'Save' }).click();

    // Row appears with deployed badge and connector health after list refresh.
    const row = page.getByRole('row', { name: /prod-edge/ });
    await expect(row).toBeVisible();
    await expect(row).toContainText('deployed');
    await expect(row).toContainText('1/1 replicas · connected');
    await expect(row).toContainText('app.example.com, *.example.com');

    expect(createBodies).toHaveLength(1);
    expect(createBodies[0]).toMatchObject({
      name: 'prod-edge',
      accountId: 'acc-123',
      zoneId: 'zone-123',
      apiToken: 'tok-abc',
      ingress: [
        { hostname: 'app.example.com', service: 'http://traefik:80' },
        { hostname: '*.example.com', service: 'http://traefik:80' },
      ],
    });
  });

  test('editing ingress rows in the dialog replaces the submitted rules', async ({ page }) => {
    await openCreateDialog(page);
    await page.getByLabel('Name', { exact: true }).fill('edge-v2');
    await page.getByLabel('Account ID', { exact: true }).fill('acc-123');
    await page.getByLabel('API Token').fill('tok-abc');

    const hostnameInputs = page.getByPlaceholder('app.example.com or *.example.com');
    const removeButtons = page.getByTitle('Remove rule');

    await hostnameInputs.first().fill('a.example.com');
    await page.getByRole('button', { name: '+ Add rule' }).click();
    await hostnameInputs.nth(1).fill('b.example.com');
    await page.getByPlaceholder('http://traefik:80').nth(1).fill('http://nginx:8080');

    // Remove the first rule; the remaining row set replaces what gets submitted.
    await removeButtons.first().click();
    await expect(hostnameInputs).toHaveCount(1);
    await expect(hostnameInputs.first()).toHaveValue('b.example.com');

    await page.getByRole('button', { name: 'Save' }).click();

    const row = page.getByRole('row', { name: /edge-v2/ });
    await expect(row).toBeVisible();

    expect(createBodies).toHaveLength(1);
    expect(createBodies[0].ingress).toEqual([
      { hostname: 'b.example.com', service: 'http://nginx:8080' },
    ]);
  });

  test('delete confirmation removes the row', async ({ page }) => {
    const seed = makeTunnel();
    tunnels.push(seed);
    await page.reload();
    const row = page.getByRole('row', { name: /seed-tunnel/ });
    await expect(row).toBeVisible();

    await row.getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('heading', { name: 'Delete Tunnel' })).toBeVisible();

    await page.getByRole('button', { name: 'Confirm' }).click();

    await expect(page.getByText('No tunnels yet.')).toBeVisible();
    await expect(page.getByRole('row', { name: /seed-tunnel/ })).toHaveCount(0);
    expect(deleteIds).toEqual([seed.id]);
    expect(deleteIds).toHaveLength(1);
  });

  test('API failure surfaces an error message', async ({ page }) => {
    createFailure = { status: 400, message: 'Cloudflare API token rejected' };

    await openCreateDialog(page);
    await page.getByLabel('Name', { exact: true }).fill('bad-edge');
    await page.getByLabel('Account ID', { exact: true }).fill('acc-123');
    await page.getByLabel('API Token').fill('tok-bad');
    await page.getByPlaceholder('app.example.com or *.example.com').first().fill('app.example.com');
    await page.getByRole('button', { name: 'Save' }).click();

    await expect(page.getByText('Cloudflare API token rejected')).toBeVisible();
    // The dialog stays open so the user can correct the input.
    await expect(page.getByRole('heading', { name: 'Create Cloudflare Tunnel' })).toBeVisible();
    expect(createBodies).toHaveLength(1);
  });
});
