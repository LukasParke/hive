<script lang="ts">
	import { authClient } from '$lib/auth-client';

	const session = authClient.useSession();

	const settingsLinks = [
		{ href: '/settings/templates', label: 'Template Sources', description: 'Manage template sources, sync from git, and custom templates' },
		{ href: '/settings/git', label: 'Git Providers', description: 'Connect GitHub or GitLab, browse repos, and enable push-to-deploy' },
		{ href: '/settings/networking', label: 'Networking', description: 'Configure ingress mode, Cloudflare tunnels, and connectivity' },
		{ href: '/settings/routing', label: 'Routing & Certificates', description: 'Manage proxy routes, SSL certificates, and middleware' },
		{ href: '/settings/notifications', label: 'Notifications', description: 'Configure Discord, Slack, Resend, webhook, and Gotify channels' },
		{ href: '/settings/backups', label: 'Backups', description: 'Manage backup schedules, volume backups, and restore history' },
		{ href: '/settings/registry', label: 'Container Registry', description: 'View registry status, browse images, and manage credentials' },
		{ href: '/settings/storage', label: 'Storage Hosts', description: 'Manage NAS, Ceph, and local storage hosts for smart volume resolution' },
		{ href: '/storage/ceph', label: 'Ceph Storage', description: 'Deploy and manage Ceph clusters for distributed HA storage' },
		{ href: '/settings/members', label: 'Members', description: 'Manage organization members and roles (RBAC)' },
		{ href: '/settings/audit', label: 'Audit', description: 'View audit log and activity history' },
		{ href: '/settings/maintenance', label: 'Maintenance', description: 'Scheduled tasks for image prune, DB vacuum, and more' },
		{ href: '/system/logs', label: 'System Logs', description: 'View system-wide logs from all services and nodes' },
	];
</script>

<div class="max-w-4xl mx-auto">
	<h2 class="text-2xl font-bold mb-6">Settings</h2>

	<div class="space-y-6">
		<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">Account</h3>
			{#if $session.data}
				<div class="space-y-3 text-sm">
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Name</span>
						<span>{$session.data.user.name}</span>
					</div>
					<div class="flex justify-between">
						<span style="color: var(--color-text-muted);">Email</span>
						<span>{$session.data.user.email}</span>
					</div>
				</div>
			{/if}
		</div>

		<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
			{#each settingsLinks as link}
				<a href={link.href} class="rounded-lg p-4 block hover:scale-[1.01] transition-transform"
					style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
					<h3 class="font-semibold mb-1">{link.label}</h3>
					<p class="text-sm" style="color: var(--color-text-muted);">{link.description}</p>
				</a>
			{/each}
		</div>

		<div class="rounded-lg p-4" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="font-semibold mb-3">Organization</h3>
			<p class="text-sm" style="color: var(--color-text-muted);">Organization and team management coming soon.</p>
		</div>

	</div>
</div>
