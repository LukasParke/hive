import Docker from 'dockerode';
import { execSync } from 'node:child_process';
import * as net from 'node:net';
import * as crypto from 'node:crypto';
import { logger } from '$lib/server/logger';

const docker = new Docker({ socketPath: '/var/run/docker.sock' });

const HIVE_NETWORK = 'hive-net';
const HIVE_IMAGE = process.env.HIVE_IMAGE || '127.0.0.1:5000/hive:latest';
function buildDatabaseURL(password: string, pgHostCount: number): string {
	const hosts = Array.from({ length: pgHostCount }, (_, i) => `hive-pg-${i}:5432`).join(',');
	return `postgres://hive:${password}@${hosts}/hive?sslmode=disable`;
}

interface LauncherOptions {
	dataDir: string;
}

async function waitForPort(host: string, port: number, timeoutMs = 60000): Promise<void> {
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			await new Promise<void>((resolve, reject) => {
				const sock = net.createConnection({ host, port }, () => {
					sock.end();
					resolve();
				});
				sock.on('error', reject);
				sock.setTimeout(2000, () => {
					sock.destroy();
					reject(new Error('timeout'));
				});
			});
			return;
		} catch {
			await new Promise((r) => setTimeout(r, 1000));
		}
	}
	throw new Error(`Port ${host}:${port} not reachable after ${timeoutMs}ms`);
}

async function waitForHTTP(url: string, timeoutMs = 60000): Promise<void> {
	const start = Date.now();
	while (Date.now() - start < timeoutMs) {
		try {
			const res = await fetch(url);
			if (res.ok) return;
		} catch {
			// retry
		}
		await new Promise((r) => setTimeout(r, 2000));
	}
	throw new Error(`HTTP ${url} not reachable after ${timeoutMs}ms`);
}

function generatePassword(): string {
	return crypto.randomBytes(24).toString('hex');
}

async function ensureSwarmSecret(name: string, value: string): Promise<string> {
	const secrets = await docker.listSecrets({ filters: { name: [name] } });
	const existing = secrets.find((s: any) => s.Spec?.Name === name);
	if (existing) {
		logger.info(`Swarm secret ${name} already exists`, { component: 'bootstrap' });
		return existing.ID as string;
	}
	const secret = await docker.createSecret({
		Name: name,
		Data: Buffer.from(value).toString('base64')
	} as any);
	logger.info(`Created Swarm secret ${name}`, { component: 'bootstrap' });
	return secret.id;
}

function swarmSecretRef(secretName: string, secretId: string) {
	return {
		File: { Name: secretName, UID: '0', GID: '0', Mode: 292 },
		SecretID: secretId,
		SecretName: secretName
	};
}

async function ensureSwarm(): Promise<void> {
	const info = await docker.info();
	if (info.Swarm?.LocalNodeState === 'active') {
		logger.info('[bootstrap] Swarm already active');
		return;
	}

	logger.info('[bootstrap] Initializing Docker Swarm...');
	await docker.swarmInit({
		ListenAddr: '0.0.0.0:2377',
		ForceNewCluster: false
	});
	logger.info('[bootstrap] Swarm initialized');
}

async function ensureNetwork(): Promise<void> {
	const networks = await docker.listNetworks({
		filters: { name: [HIVE_NETWORK] }
	});

	if (networks.some((n) => n.Name === HIVE_NETWORK)) {
		logger.info('[bootstrap] Network already exists');
		return;
	}

	logger.info('[bootstrap] Creating overlay network...');
	await docker.createNetwork({
		Name: HIVE_NETWORK,
		Driver: 'overlay',
		Attachable: true,
		Labels: { 'hive.managed': 'true' }
	});
	logger.info('[bootstrap] Network created');
}

interface PostgresHAPasswords {
	pgPassword: string;
	repmgrPassword: string;
	pgAdminPassword: string;
}

async function getManagerNodes(): Promise<{ hostname: string; id: string }[]> {
	const nodes = await docker.listNodes();
	return nodes
		.filter(
			(n: any) =>
				n.Spec?.Role === 'manager' &&
				n.Status?.State === 'ready' &&
				n.Description?.Hostname
		)
		.map((n: any) => ({
			hostname: n.Description.Hostname as string,
			id: n.ID as string
		}))
		.sort((a, b) => a.hostname.localeCompare(b.hostname));
}

async function ensurePostgresHA(
	passwords: PostgresHAPasswords,
	managers: { hostname: string }[]
): Promise<void> {
	if (managers.length === 0) {
		throw new Error('No manager nodes found for PostgreSQL HA cluster');
	}

	const serviceNames = managers.map((_m, i) => `hive-pg-${i}`);
	const partnerNodes = serviceNames.join(',');
	const primaryHost = serviceNames[0];

	const existingServices = await docker.listServices();
	const existingNames = new Set(existingServices.map((s: any) => s.Spec?.Name));

	for (let i = 0; i < managers.length; i++) {
		const manager = managers[i];
		const serviceName = serviceNames[i];
		const nodeId = 1000 + i;

		if (existingNames.has(serviceName)) {
			logger.info(`${serviceName} already exists`, { component: 'bootstrap' });
			continue;
		}

		logger.info(`Deploying ${serviceName} on ${manager.hostname}`, { component: 'bootstrap' });
		await docker.createService({
			Name: serviceName,
			Labels: {
				'hive.managed': 'true',
				'hive.role': 'postgres-ha',
				'hive.pg.hostname': manager.hostname
			},
			TaskTemplate: {
				ContainerSpec: {
					Image: 'bitnamilegacy/postgresql-repmgr:latest',
					Env: [
						`POSTGRESQL_POSTGRES_PASSWORD=${passwords.pgAdminPassword}`,
						`POSTGRESQL_USERNAME=hive`,
						`POSTGRESQL_PASSWORD=${passwords.pgPassword}`,
						`POSTGRESQL_DATABASE=hive`,
						`REPMGR_PASSWORD=${passwords.repmgrPassword}`,
						`REPMGR_PRIMARY_HOST=${primaryHost}`,
						`REPMGR_PARTNER_NODES=${partnerNodes}`,
						`REPMGR_NODE_NAME=${serviceName}`,
						`REPMGR_NODE_NETWORK_NAME=${serviceName}`,
						`REPMGR_NODE_ID=${nodeId}`,
						`POSTGRESQL_NUM_SYNCHRONOUS_REPLICAS=1`
					],
					Mounts: [
						{
							Type: 'volume',
							Source: `hive-pg-${i}-data`,
							Target: '/bitnami/postgresql'
						}
					],
					HealthCheck: {
						Test: ['CMD-SHELL', 'pg_isready -U hive -d hive'],
						Interval: 10_000_000_000,
						Timeout: 5_000_000_000,
						Retries: 10,
						StartPeriod: 60_000_000_000
					}
				},
				Networks: [{ Target: HIVE_NETWORK }],
				Placement: {
					Constraints: ['node.role==manager', `node.hostname==${manager.hostname}`]
				},
				RestartPolicy: { Condition: 'on-failure', MaxAttempts: 10 }
			},
			Mode: { Replicated: { Replicas: 1 } },
			EndpointSpec: { Ports: [] }
		} as any);
		logger.info(`${serviceName} deployed`, { component: 'bootstrap' });
	}

}

interface SecretRefs {
	dbPasswordId: string;
	engineSecretId: string;
	authSecretId: string;
}

async function ensureEngine(
	dbURL: string,
	natsURL: string,
	secretRefs: SecretRefs,
	managerCount = 1
): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-engine'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-engine')) {
		logger.info('[bootstrap] Engine service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying hive-engine...');
	await docker.createService({
		Name: 'hive-engine',
		Labels: {
			'hive.managed': 'true',
			'traefik.enable': 'true',
			// Route /api/v1/* to Go engine (higher priority than manager catch-all)
			'traefik.http.routers.hive-api.rule': 'PathPrefix(`/api/v1/`)',
			'traefik.http.routers.hive-api.entrypoints': 'web',
			'traefik.http.routers.hive-api.priority': '20',
			'traefik.http.services.hive-api.loadbalancer.server.port': '9090',
			// Route /ws/* to Go engine for WebSockets
			'traefik.http.routers.hive-ws.rule': 'PathPrefix(`/ws/`)',
			'traefik.http.routers.hive-ws.entrypoints': 'web',
			'traefik.http.routers.hive-ws.priority': '20',
			'traefik.http.routers.hive-ws.service': 'hive-api',
			// Route /engine/v1/* for internal engine API
			'traefik.http.routers.hive-engine-internal.rule': 'PathPrefix(`/engine/v1/`)',
			'traefik.http.routers.hive-engine-internal.entrypoints': 'web',
			'traefik.http.routers.hive-engine-internal.priority': '20',
			'traefik.http.routers.hive-engine-internal.service': 'hive-api'
		},
		TaskTemplate: {
			ContainerSpec: {
				Image: HIVE_IMAGE,
				Env: [
					'HIVE_ROLE=engine',
					`DATABASE_URL=${dbURL}`,
					`HIVE_NATS_URL=${natsURL}`
				],
				Secrets: [
					swarmSecretRef('hive-db-password', secretRefs.dbPasswordId),
					swarmSecretRef('hive-engine-secret', secretRefs.engineSecretId)
				],
				Mounts: [
					{
						Type: 'bind',
						Source: '/var/run/docker.sock',
						Target: '/var/run/docker.sock'
					},
					{
						Type: 'volume',
						Source: 'hive-backups',
						Target: '/data/backups'
					}
				],
				HealthCheck: {
					Test: ['CMD', 'curl', '-f', 'http://localhost:9090/engine/v1/healthz'],
					Interval: 10_000_000_000,
					Timeout: 5_000_000_000,
					Retries: 3,
					StartPeriod: 15_000_000_000
				}
			},
			Networks: [{ Target: HIVE_NETWORK }],
			Placement: {
				Constraints: ['node.role==manager'],
				MaxReplicas: 1
			},
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Replicated: { Replicas: Math.min(managerCount, 3) } },
		EndpointSpec: { Ports: [] }
	} as any);

	logger.info(`[bootstrap] hive-engine deployed (${Math.min(managerCount, 3)} replicas)`);
}

async function ensureNATS(): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-nats'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-nats')) {
		logger.info('[bootstrap] NATS service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying NATS...');
	await docker.createService({
		Name: 'hive-nats',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: 'nats:2-alpine',
				Args: ['-js', '-sd', '/data', '-m', '8222'],
				Mounts: [
					{
						Type: 'volume',
						Source: 'hive-nats-data',
						Target: '/data'
					}
				],
				HealthCheck: {
					Test: ['CMD', 'wget', '--spider', '-q', 'http://localhost:8222/healthz'],
					Interval: 10_000_000_000,
					Timeout: 5_000_000_000,
					Retries: 3,
					StartPeriod: 10_000_000_000
				}
			},
			Networks: [{ Target: HIVE_NETWORK }],
			Placement: {
				Constraints: ['node.role==manager']
			},
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Replicated: { Replicas: 1 } },
		EndpointSpec: { Ports: [] }
	} as any);
	logger.info('[bootstrap] NATS deployed');
}

async function ensureTraefik(managerCount = 1): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-traefik'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-traefik')) {
		logger.info('[bootstrap] Traefik service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying Traefik...');
	await docker.createService({
		Name: 'hive-traefik',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: 'traefik:v3.6',
				Args: [
					'--providers.swarm.endpoint=unix:///var/run/docker.sock',
					'--providers.swarm.exposedByDefault=false',
					'--providers.swarm.network=hive-net',
					'--providers.file.directory=/dynamic',
					'--providers.file.watch=true',
					'--entrypoints.web.address=:80',
					'--entrypoints.web.http.redirections.entrypoint.to=websecure',
					'--entrypoints.web.http.redirections.entrypoint.scheme=https',
					'--entrypoints.websecure.address=:443',
					'--ping=true',
					'--ping.entrypoint=web',
					'--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web',
					'--certificatesresolvers.letsencrypt.acme.storage=/certs/acme.json',
					'--certificatesresolvers.cloudflare.acme.dnschallenge.provider=cloudflare',
					'--certificatesresolvers.cloudflare.acme.dnschallenge.resolvers=1.1.1.1:53,8.8.8.8:53',
					'--certificatesresolvers.cloudflare.acme.storage=/certs/acme-cf.json'
				],
				Env: [
					...(process.env.HIVE_CF_API_TOKEN ? [`CF_DNS_API_TOKEN=${process.env.HIVE_CF_API_TOKEN}`] : [])
				],
				Mounts: [
					{
						Type: 'bind',
						Source: '/var/run/docker.sock',
						Target: '/var/run/docker.sock',
						ReadOnly: true
					},
					{
						Type: 'volume',
						Source: 'hive-traefik-certs',
						Target: '/certs'
					},
					{
						Type: 'volume',
						Source: 'hive-traefik-dynamic',
						Target: '/dynamic'
					}
				],
				HealthCheck: {
					Test: ['CMD', 'traefik', 'healthcheck', '--ping'],
					Interval: 10_000_000_000,
					Timeout: 5_000_000_000,
					Retries: 3,
					StartPeriod: 10_000_000_000
				}
			},
			Networks: [{ Target: HIVE_NETWORK }],
			Placement: {
				Constraints: ['node.role==manager'],
				MaxReplicas: 1
			},
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Replicated: { Replicas: Math.min(managerCount, 3) } },
		EndpointSpec: {
			Ports: [
				{ Protocol: 'tcp', TargetPort: 80, PublishedPort: 80, PublishMode: 'ingress' },
				{ Protocol: 'tcp', TargetPort: 443, PublishedPort: 443, PublishMode: 'ingress' }
			]
		}
	} as any);
	logger.info(`[bootstrap] Traefik deployed (${Math.min(managerCount, 3)} replicas)`);
}

async function ensureNodeExporter(): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-node-exporter'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-node-exporter')) {
		logger.info('[bootstrap] Node exporter service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying node-exporter...');
	await docker.createService({
		Name: 'hive-node-exporter',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: 'prom/node-exporter:latest',
				Args: [
					'--path.procfs=/host/proc',
					'--path.sysfs=/host/sys',
					'--path.rootfs=/rootfs',
					'--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)'
				],
				Mounts: [
					{ Type: 'bind', Source: '/proc', Target: '/host/proc', ReadOnly: true },
					{ Type: 'bind', Source: '/sys', Target: '/host/sys', ReadOnly: true },
					{ Type: 'bind', Source: '/', Target: '/rootfs', ReadOnly: true }
				]
			},
			Networks: [{ Target: HIVE_NETWORK }],
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Global: {} },
		EndpointSpec: {
			Ports: [
				{ Protocol: 'tcp', TargetPort: 9100, PublishedPort: 9100, PublishMode: 'host' }
			]
		}
	} as any);
	logger.info('[bootstrap] node-exporter deployed');
}

async function ensureCadvisor(): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-cadvisor'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-cadvisor')) {
		logger.info('[bootstrap] cAdvisor service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying cAdvisor...');
	await docker.createService({
		Name: 'hive-cadvisor',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: 'gcr.io/cadvisor/cadvisor:latest',
				Mounts: [
					{ Type: 'bind', Source: '/', Target: '/rootfs', ReadOnly: true },
					{ Type: 'bind', Source: '/var/run', Target: '/var/run', ReadOnly: false },
					{ Type: 'bind', Source: '/sys', Target: '/sys', ReadOnly: true },
					{ Type: 'bind', Source: '/var/lib/docker/', Target: '/var/lib/docker', ReadOnly: true }
				]
			},
			Networks: [{ Target: HIVE_NETWORK }],
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Global: {} },
		EndpointSpec: {
			Ports: [
				{ Protocol: 'tcp', TargetPort: 8080, PublishedPort: 8180, PublishMode: 'host' }
			]
		}
	} as any);
	logger.info('[bootstrap] cAdvisor deployed');
}

async function ensurePrometheus(): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-prometheus'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-prometheus')) {
		logger.info('[bootstrap] Prometheus service already exists');
		return;
	}

	const promConfig = `
global:
  scrape_interval: 5s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'node-exporter'
    dockerswarm_sd_configs:
      - host: unix:///var/run/docker.sock
        role: nodes
    relabel_configs:
      - source_labels: [__meta_dockerswarm_node_address]
        target_label: __address__
        replacement: '\${1}:9100'
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: node_hostname
      - source_labels: [__meta_dockerswarm_node_id]
        target_label: node_id
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: instance

  - job_name: 'cadvisor'
    dockerswarm_sd_configs:
      - host: unix:///var/run/docker.sock
        role: nodes
    relabel_configs:
      - source_labels: [__meta_dockerswarm_node_address]
        target_label: __address__
        replacement: '\${1}:8180'
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: node_hostname
      - source_labels: [__meta_dockerswarm_node_id]
        target_label: node_id
      - source_labels: [__meta_dockerswarm_node_hostname]
        target_label: instance
`;

	// Create Docker config for prometheus.yml
	const configName = `hive-prometheus-config-${Date.now()}`;
	await docker.createConfig({
		Name: configName,
		Data: Buffer.from(promConfig).toString('base64'),
		Labels: { 'hive.managed': 'true' }
	} as any);

	logger.info('[bootstrap] Deploying Prometheus...');
	await docker.createService({
		Name: 'hive-prometheus',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: 'prom/prometheus:latest',
				User: '0',
				Args: [
					'--config.file=/etc/prometheus/prometheus.yml',
					'--storage.tsdb.path=/prometheus',
					'--storage.tsdb.retention.time=15d',
					'--web.console.libraries=/usr/share/prometheus/console_libraries',
					'--web.console.templates=/usr/share/prometheus/consoles'
				],
				Mounts: [
					{
						Type: 'volume',
						Source: 'hive-prometheus-data',
						Target: '/prometheus'
					},
					{
						Type: 'bind',
						Source: '/var/run/docker.sock',
						Target: '/var/run/docker.sock',
						ReadOnly: true
					}
				],
				Configs: [
					{
						File: { Name: '/etc/prometheus/prometheus.yml', UID: '0', GID: '0', Mode: 0o444 },
						ConfigName: configName
					}
				]
			},
			Networks: [{ Target: HIVE_NETWORK }],
			Placement: {
				Constraints: ['node.role==manager']
			},
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Replicated: { Replicas: 1 } },
		EndpointSpec: { Ports: [] }
	} as any);
	logger.info('[bootstrap] Prometheus deployed');
}

async function ensureAgent(natsURL: string): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-agent'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-agent')) {
		logger.info('[bootstrap] Agent service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying hive-agent...');
	await docker.createService({
		Name: 'hive-agent',
		Labels: { 'hive.managed': 'true' },
		TaskTemplate: {
			ContainerSpec: {
				Image: HIVE_IMAGE,
				Env: [
					'HIVE_ROLE=agent',
					`HIVE_NATS_URL=${natsURL}`,
					'NODE_HOSTNAME={{.Node.Hostname}}'
				],
				Mounts: [
					{
						Type: 'bind',
						Source: '/var/run/docker.sock',
						Target: '/var/run/docker.sock'
					},
					{
						Type: 'bind',
						Source: '/',
						Target: '/rootfs',
						ReadOnly: true
					}
				],
				Privileges: {
					PidMode: 'host'
				},
				CapabilityAdd: ['SYS_ADMIN']
			},
			Networks: [{ Target: HIVE_NETWORK }],
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Global: {} },
		EndpointSpec: { Ports: [] }
	} as any);
	logger.info('[bootstrap] hive-agent deployed');
}

interface ManagerCreateOpts {
	dbURL: string;
	hiveURL: string;
	secretRefs: SecretRefs;
}

async function ensureManager(opts: ManagerCreateOpts, managerCount = 1): Promise<void> {
	const services = await docker.listServices({
		filters: { name: ['hive-manager'] }
	});

	if (services.some((s) => s.Spec?.Name === 'hive-manager')) {
		logger.info('[bootstrap] Manager service already exists');
		return;
	}

	logger.info('[bootstrap] Deploying hive-manager (SvelteKit)...');
	await docker.createService({
		Name: 'hive-manager',
		Labels: {
			'hive.managed': 'true',
			'traefik.enable': 'true',
			// Catch-all for UI pages and auth routes (lower priority than API routes)
			'traefik.http.routers.hive.rule': 'PathPrefix(`/`)',
			'traefik.http.routers.hive.entrypoints': 'web',
			'traefik.http.routers.hive.priority': '1',
			'traefik.http.services.hive.loadbalancer.server.port': '8080',
			// Auth routes stay on SvelteKit (higher priority than the Go API catch)
			'traefik.http.routers.hive-auth.rule': 'PathPrefix(`/api/auth/`)',
			'traefik.http.routers.hive-auth.entrypoints': 'web',
			'traefik.http.routers.hive-auth.priority': '30',
			'traefik.http.routers.hive-auth.service': 'hive'
		},
		TaskTemplate: {
			ContainerSpec: {
				Image: HIVE_IMAGE,
				Env: [
					'HIVE_ROLE=manager',
					'HIVE_MANAGED=true',
					`DATABASE_URL=${opts.dbURL}`,
					'HIVE_ENGINE_URL=http://hive-engine:9090',
					`BETTER_AUTH_URL=${opts.hiveURL}`,
					`HIVE_URL=${opts.hiveURL}`,
					`ORIGIN=${opts.hiveURL}`,
					'PORT=8080',
					'NODE_ENV=production'
				],
				Secrets: [
					swarmSecretRef('hive-db-password', opts.secretRefs.dbPasswordId),
					swarmSecretRef('hive-engine-secret', opts.secretRefs.engineSecretId),
					swarmSecretRef('hive-auth-secret', opts.secretRefs.authSecretId)
				],
				HealthCheck: {
					Test: ['CMD', 'curl', '-f', 'http://localhost:8080/healthz'],
					Interval: 10_000_000_000,
					Timeout: 5_000_000_000,
					Retries: 3,
					StartPeriod: 30_000_000_000
				}
			},
			Networks: [{ Target: HIVE_NETWORK }],
			Placement: {
				Constraints: ['node.role==manager'],
				MaxReplicas: 1
			},
			RestartPolicy: { Condition: 'on-failure', MaxAttempts: 5 }
		},
		Mode: { Replicated: { Replicas: Math.min(managerCount, 3) } },
		EndpointSpec: {
			Ports: [
				{ Protocol: 'tcp', TargetPort: 8080, PublishedPort: 8080, PublishMode: 'ingress' }
			]
		}
	} as any);
	logger.info(`[bootstrap] hive-manager deployed (${Math.min(managerCount, 3)} replicas)`);
}

export async function runLauncher(options: LauncherOptions): Promise<void> {
	logger.info('[bootstrap] Starting Hive launcher...');

	const externalDBURL = process.env.EXTERNAL_DATABASE_URL;
	const hiveURL = process.env.HIVE_URL || 'http://localhost:8080';

	await ensureSwarm();
	await ensureNetwork();

	let dbURL: string;
	let managerCount = 1;

	if (externalDBURL) {
		logger.info('[bootstrap] Using external database');
		dbURL = externalDBURL;
		const mgrs = await getManagerNodes();
		managerCount = Math.max(mgrs.length, 1);
	} else {
		const pgPassword = generatePassword();
		const repmgrPassword = generatePassword();
		const pgAdminPassword = generatePassword();
		const managers = await getManagerNodes();
		managerCount = Math.max(managers.length, 1);
		dbURL = buildDatabaseURL(pgPassword, managerCount);
		logger.info(
			`Found ${managers.length} manager node(s): ${managers.map((m) => m.hostname).join(', ')}`
		);

		await ensurePostgresHA(
			{ pgPassword, repmgrPassword, pgAdminPassword },
			managers
		);

		logger.info('[bootstrap] Waiting for PostgreSQL HA primary (hive-pg-0)...');
		await waitForPort('hive-pg-0', 5432, 180000);
		logger.info('[bootstrap] PostgreSQL HA primary is ready');
	}

	logger.info('[bootstrap] Running Prisma migrations...');
	try {
		execSync(`npx prisma migrate deploy`, {
			env: { ...process.env, DATABASE_URL: dbURL },
			stdio: 'inherit'
		});
	} catch (e) {
		logger.warn('[bootstrap] Migration warning:', e);
	}

	await ensureNATS();
	logger.info('[bootstrap] Waiting for NATS...');
	await waitForPort('hive-nats', 4222, 60000);
	logger.info('[bootstrap] NATS is ready');

	const natsURL = 'nats://hive-nats:4222';
	const engineSecret = generatePassword();
	const authSecret = generatePassword();

	logger.info('[bootstrap] Creating Docker Swarm secrets...');
	const pgPassword = dbURL.match(/:([^@]+)@/)?.[1] || generatePassword();
	const dbPasswordId = await ensureSwarmSecret('hive-db-password', pgPassword);
	const engineSecretId = await ensureSwarmSecret('hive-engine-secret', engineSecret);
	const authSecretId = await ensureSwarmSecret('hive-auth-secret', authSecret);
	const secretRefs: SecretRefs = { dbPasswordId, engineSecretId, authSecretId };

	await ensureEngine(dbURL, natsURL, secretRefs, managerCount);
	logger.info('[bootstrap] Waiting for hive-engine...');
	await waitForHTTP('http://hive-engine:9090/engine/v1/healthz', 120000);
	logger.info('[bootstrap] hive-engine is ready');

	await ensureAgent(natsURL);
	await ensureNodeExporter();
	await ensureCadvisor();
	await ensurePrometheus();
	logger.info('[bootstrap] Waiting for Prometheus...');
	await waitForHTTP('http://hive-prometheus:9090/-/healthy', 60000);
	logger.info('[bootstrap] Prometheus is ready');

	await ensureTraefik(managerCount);
	await ensureManager({ dbURL, hiveURL, secretRefs }, managerCount);

	logger.info('[bootstrap] All services deployed. Hive is running!');
	logger.info('[bootstrap] Launcher exiting (hive-manager is now a Swarm service)');
}
