import { createApi } from '$lib/api';
import { safeLoad } from '$lib/load-utils';
import type { App, BackupConfig, Stack, SystemTask, UpdatesSummary } from '$lib/types';

export const load = async ({ fetch }: { fetch: typeof globalThis.fetch }) => {
	const api = createApi(fetch);
	const [apps, stacks, tasks, updates, backups] = await Promise.all([
		safeLoad<(App & { project_name: string })[]>(() => api.listAllApps(), []),
		safeLoad<(Stack & { project_name: string })[]>(() => api.listAllStacks(), []),
		safeLoad<SystemTask[]>(() => api.listSystemTasks(), []),
		safeLoad<UpdatesSummary>(
			() => api.updatesSummary(),
			{ nodes_total: 0, pending_updates: 0, security_updates: 0, reboot_required: 0, service_updates: 0 }
		),
		safeLoad<BackupConfig[]>(() => api.listBackupConfigs(), []),
	]);
	const [gitSources, dnsProviders, networking] = await Promise.all([
		safeLoad(() => api.listGitSources(), []),
		safeLoad(() => api.listDNSProviders(), []),
		safeLoad(() => api.getNetworkingSettings(), {}),
	]);
	return { apps, stacks, tasks, updates, backups, gitSources, dnsProviders, networking };
};
