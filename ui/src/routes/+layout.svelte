<script lang="ts">
	import '../app.css';
	import { HiveLogo } from '$lib/components';
	import { authClient } from '$lib/auth-client';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { Toaster } from 'svelte-sonner';
	import CommandPalette from '$lib/components/CommandPalette.svelte';
	import { onMount } from 'svelte';

	let { data, children } = $props();
	let sidebarCollapsed = $state(false);
	let mobileOpen = $state(false);
	let commandPaletteOpen = $state(false);

	onMount(() => {
		function handleKeydown(e: KeyboardEvent) {
			if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
				e.preventDefault();
				commandPaletteOpen = !commandPaletteOpen;
			}
		}
		window.addEventListener('keydown', handleKeydown);
		return () => window.removeEventListener('keydown', handleKeydown);
	});

	const navSections = [
		{
			items: [
				{ href: '/', label: 'Dashboard', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>` },
				{ href: '/projects', label: 'Projects', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>` },
				{ href: '/apps', label: 'Apps', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><line x1="4" y1="10" x2="20" y2="10"/><line x1="10" y1="4" x2="10" y2="20"/></svg>` },
				{ href: '/nodes', label: 'Nodes', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><circle cx="6" cy="6" r="1" fill="currentColor"/><circle cx="6" cy="18" r="1" fill="currentColor"/></svg>` },
				{ href: '/catalog', label: 'Catalog', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>` },
				{ href: '/bespoke', label: 'Bespoke Apps', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3l9 4.5-9 4.5-9-4.5L12 3z"/><path d="M3 16.5L12 21l9-4.5"/><path d="M3 12l9 4.5 9-4.5"/></svg>` },
			]
		},
		{
			label: 'Operations',
			items: [
				{ href: '/operations', label: 'Operations Center', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3 13h4l2-6 4 12 2-6h6"/><circle cx="6" cy="6" r="2"/><circle cx="18" cy="6" r="2"/></svg>` },
				{ href: '/updates', label: 'Updates', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 2v6h-6"/><path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M3 22v-6h6"/><path d="M21 12a9 9 0 0 1-15 6.7L3 16"/></svg>` },
				{ href: '/backups', label: 'Backups', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>` },
				{ href: '/security', label: 'Security', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>` },
				{ href: '/maintenance', label: 'Maintenance', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/></svg>` },
				{ href: '/logs', label: 'Logs', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>` },
				{ href: '/system-tasks', label: 'System Tasks', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>` },
			]
		},
		{
			label: 'Infrastructure',
			items: [
				{ href: '/networks', label: 'Networks', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>` },
				{ href: '/storage-hosts', label: 'Storage Hosts', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><circle cx="6" cy="6" r="1" fill="currentColor"/><circle cx="6" cy="18" r="1" fill="currentColor"/></svg>` },
				{ href: '/ceph', label: 'Ceph', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>` },
				{ href: '/dns', label: 'DNS', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>` },
				{ href: '/networking', label: 'Ingress', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M16 3h5v5"/><line x1="4" y1="20" x2="21" y2="3"/><path d="M21 16v5h-5"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/></svg>` },
				{ href: '/registry', label: 'Registry', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/></svg>` },
				{ href: '/vpn', label: 'VPN', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>` },
				{ href: '/ups', label: 'UPS', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z"/></svg>` },
			]
		},
		{
			label: 'Admin',
			items: [
				{ href: '/members', label: 'Members', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>` },
				{ href: '/routing', label: 'Routing', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M16 3h5v5"/><line x1="4" y1="20" x2="21" y2="3"/><path d="M21 16v5h-5"/><line x1="15" y1="15" x2="21" y2="21"/><line x1="4" y1="4" x2="9" y2="9"/></svg>` },
				{ href: '/notifications', label: 'Notifications', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>` },
				{ href: '/audit', label: 'Audit', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"/><rect x="9" y="3" width="6" height="4" rx="1"/><line x1="9" y1="12" x2="15" y2="12"/><line x1="9" y1="16" x2="15" y2="16"/></svg>` },
				{ href: '/git', label: 'Git', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="4"/><line x1="1.05" y1="12" x2="7" y2="12"/><line x1="17.01" y1="12" x2="22.96" y2="12"/></svg>` },
				{ href: '/templates', label: 'Templates', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>` },
				{ href: '/api-tokens', label: 'API Tokens', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>` },
				{ href: '/webhooks', label: 'Webhooks', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>` },
				{ href: '/settings', label: 'Settings', icon: `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>` },
			]
		}
	];

	interface BreadcrumbItem { label: string; href?: string }

	let breadcrumbs = $derived.by((): BreadcrumbItem[] => {
		const path = page.url.pathname;
		if (path === '/') return [];

		const crumbs: BreadcrumbItem[] = [];
		const segments = path.split('/').filter(Boolean);

		const labelMap: Record<string, string> = {
			projects: 'Projects',
			nodes: 'Nodes',
			catalog: 'Catalog',
			bespoke: 'Bespoke Apps',
			operations: 'Operations',
			settings: 'Settings',
			updates: 'Updates',
			ceph: 'Ceph',
			logs: 'Logs',
			apps: 'Apps',
			secrets: 'Secrets',
			volumes: 'Volumes',
			stacks: 'Stacks',
			env: 'Environment',
			auth: 'Auth',
			login: 'Login',
			register: 'Register',
			networks: 'Networks',
			configs: 'Configs',
			jobs: 'Jobs',
			security: 'Security',
			vpn: 'VPN',
			ups: 'UPS',
			webhooks: 'Webhooks',
			'api-tokens': 'API Tokens',
			clusters: 'Clusters',
			members: 'Members',
			dns: 'DNS',
			git: 'Git',
			networking: 'Networking',
			notifications: 'Notifications',
			registry: 'Registry',
			routing: 'Routing',
			backups: 'Backups',
			maintenance: 'Maintenance',
			audit: 'Audit',
			templates: 'Templates',
			deploy: 'Deploy',
			'storage-hosts': 'Storage Hosts',
		};

		let builtPath = '';
		for (let i = 0; i < segments.length; i++) {
			const seg = segments[i];
			builtPath += '/' + seg;
			const isLast = i === segments.length - 1;
			const label = labelMap[seg] ?? (seg.length > 20 ? seg.slice(0, 8) + '...' : seg);
			crumbs.push({
				label,
				href: isLast ? undefined : builtPath,
			});
		}
		return crumbs;
	});

	function isActive(href: string): boolean {
		if (href === '/') return page.url.pathname === '/';
		return page.url.pathname.startsWith(href);
	}

	async function handleSignOut() {
		try {
			await authClient.signOut();
		} catch (e) {
			console.error('[hive] Sign out failed:', e);
		}
		goto('/auth/login');
	}

	function closeMobileSidebar() {
		mobileOpen = false;
	}
</script>

<Toaster position="top-right" theme="dark" richColors />

{#if page.url.pathname.startsWith('/auth') || page.url.pathname.startsWith('/healthz')}
	{@render children()}
{:else if data.user}
	<div class="layout">
		<!-- Mobile overlay -->
		{#if mobileOpen}
			<div
				class="mobile-overlay"
				role="button"
				tabindex="0"
				aria-label="Close menu"
				onclick={closeMobileSidebar}
				onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && closeMobileSidebar()}
			></div>
		{/if}

		<aside class="sidebar" class:collapsed={sidebarCollapsed} class:mobile-open={mobileOpen}>
			<div class="sidebar-header">
				<a href="/" class="sidebar-brand" onclick={closeMobileSidebar}>
					<HiveLogo size={26} />
					{#if !sidebarCollapsed}
						<div>
							<h1 class="brand-title">Hive</h1>
							<p class="brand-subtitle">Swarm Orchestrator</p>
						</div>
					{/if}
				</a>
				<button
					class="btn btn-ghost btn-icon btn-sm collapse-toggle desktop-only"
					onclick={() => sidebarCollapsed = !sidebarCollapsed}
					aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
				>
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
						{#if sidebarCollapsed}
							<path d="M13 17l5-5-5-5M6 17l5-5-5-5"/>
						{:else}
							<path d="M11 17l-5-5 5-5M18 17l-5-5 5-5"/>
						{/if}
					</svg>
				</button>
				<button class="btn btn-ghost btn-icon btn-sm mobile-only" onclick={closeMobileSidebar} aria-label="Close menu">
					<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
				</button>
			</div>

			<nav class="sidebar-nav">
				{#each navSections as section}
					{#if section.label && !sidebarCollapsed}
						<div class="nav-section-label">{section.label}</div>
					{/if}
					{#each section.items as item}
						<a
							href={item.href}
							class="nav-item"
							class:active={isActive(item.href)}
							title={sidebarCollapsed ? item.label : undefined}
							onclick={closeMobileSidebar}
						>
							<span class="nav-icon">{@html item.icon}</span>
							{#if !sidebarCollapsed}
								<span class="nav-label">{item.label}</span>
							{/if}
						</a>
					{/each}
				{/each}
			</nav>

			<div class="sidebar-footer">
				{#if !sidebarCollapsed}
					<div class="user-info">
						<div class="user-avatar">
							{(data.user.name || data.user.email || '?')[0].toUpperCase()}
						</div>
						<div class="truncate" style="flex: 1; min-width: 0">
							<p class="user-name truncate">{data.user.name || data.user.email}</p>
						</div>
					</div>
				{/if}
				<button onclick={handleSignOut} class="btn btn-ghost btn-sm" title="Sign out">
					<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
					{#if !sidebarCollapsed}
						<span>Sign out</span>
					{/if}
				</button>
			</div>
		</aside>

		<div class="main-area">
			<!-- Top bar -->
			<header class="top-bar">
				<div class="flex items-center gap-3">
					<button class="btn btn-ghost btn-icon btn-sm mobile-only" onclick={() => mobileOpen = true} aria-label="Open menu">
						<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
					</button>
					{#if breadcrumbs.length > 0}
						<nav class="breadcrumbs" aria-label="Breadcrumb">
							<a href="/">Home</a>
							{#each breadcrumbs as crumb}
								<span class="separator">/</span>
								{#if crumb.href}
									<a href={crumb.href}>{crumb.label}</a>
								{:else}
									<span class="current">{crumb.label}</span>
								{/if}
							{/each}
						</nav>
					{/if}
				</div>
				<button
					class="hidden md:flex quick-search-trigger"
					onclick={() => commandPaletteOpen = true}
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z"/>
					</svg>
					<span>Search</span>
					<kbd class="kbd-chip">Ctrl+K</kbd>
				</button>
			</header>

			<main class="main-content">
				{@render children()}
			</main>
		</div>

		<CommandPalette bind:open={commandPaletteOpen} />
	</div>
{/if}

<style>
	.layout {
		display: flex;
		height: 100vh;
		overflow: hidden;
	}

	.sidebar {
		width: 15rem;
		display: flex;
		flex-direction: column;
		background-color: var(--color-surface);
		border-right: 1px solid var(--color-border);
		transition: width var(--transition-slow);
		flex-shrink: 0;
		z-index: 150;
	}

	.sidebar.collapsed {
		width: 4rem;
	}

	.sidebar-header {
		padding: var(--space-md);
		border-bottom: 1px solid var(--color-border);
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-sm);
		min-height: 3.5rem;
	}

	.sidebar-brand {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		text-decoration: none;
		color: inherit;
	}

	.brand-title {
		font-size: var(--text-lg);
		font-weight: 700;
		background: linear-gradient(90deg, var(--color-primary) 0%, #f5d78e 50%, var(--color-primary) 100%);
		background-size: 200% auto;
		background-clip: text;
		-webkit-background-clip: text;
		-webkit-text-fill-color: transparent;
		animation: shimmer 6s linear infinite;
		line-height: 1.2;
	}

	.brand-subtitle {
		font-size: 0.6rem;
		text-transform: uppercase;
		letter-spacing: 0.1em;
		color: var(--color-text-muted);
	}

	.collapse-toggle {
		opacity: 0;
		transition: opacity var(--transition-base);
	}
	.sidebar:hover .collapse-toggle {
		opacity: 1;
	}

	.sidebar-nav {
		flex: 1;
		padding: var(--space-sm);
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.nav-section-label {
		font-size: 0.65rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--color-text-disabled);
		padding: var(--space-md) var(--space-sm) var(--space-xs);
	}

	.nav-item {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		padding: var(--space-sm) var(--space-sm);
		border-radius: var(--radius-md);
		font-size: var(--text-sm);
		color: var(--color-text-muted);
		text-decoration: none;
		transition: all var(--transition-fast);
	}

	.nav-item:hover {
		background-color: var(--color-surface-hover);
		color: var(--color-text);
	}

	.nav-item.active {
		background-color: var(--color-primary-dim);
		color: var(--color-text);
		font-weight: 500;
	}

	.nav-icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		flex-shrink: 0;
	}

	.nav-item.active .nav-icon {
		color: var(--color-primary);
	}

	.sidebar-footer {
		padding: var(--space-sm) var(--space-md);
		border-top: 1px solid var(--color-border);
		display: flex;
		flex-direction: column;
		gap: var(--space-xs);
	}

	.user-info {
		display: flex;
		align-items: center;
		gap: var(--space-sm);
		padding: var(--space-xs) 0;
	}

	.user-avatar {
		width: 28px;
		height: 28px;
		border-radius: 50%;
		background-color: var(--color-primary-dim);
		color: var(--color-primary);
		display: flex;
		align-items: center;
		justify-content: center;
		font-size: var(--text-xs);
		font-weight: 600;
		flex-shrink: 0;
	}

	.user-name {
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--color-text-secondary);
	}

	.main-area {
		flex: 1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		min-width: 0;
	}

	.top-bar {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.5rem var(--space-xl);
		border-bottom: 1px solid var(--color-border);
		background-color: var(--color-surface);
		min-height: 2.75rem;
		flex-shrink: 0;
	}

	.main-content {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-xl);
	}

	.mobile-overlay {
		display: none;
	}

	.mobile-only {
		display: none !important;
	}

	.desktop-only {
		display: inline-flex;
	}

	@media (max-width: 768px) {
		.sidebar {
			position: fixed;
			left: 0;
			top: 0;
			bottom: 0;
			width: 16rem !important;
			transform: translateX(-100%);
			transition: transform var(--transition-slow);
		}
		.sidebar.mobile-open {
			transform: translateX(0);
		}
		.sidebar.collapsed {
			width: 16rem !important;
		}
		.mobile-overlay {
			display: block;
			position: fixed;
			inset: 0;
			background-color: rgba(0, 0, 0, 0.5);
			z-index: 140;
			animation: fade-in var(--transition-base);
		}
		.mobile-only {
			display: inline-flex !important;
		}
		.desktop-only {
			display: none !important;
		}
		.main-content {
			padding: var(--space-md);
		}
		.top-bar {
			padding: 0.5rem var(--space-md);
		}
		.nav-item {
			padding: var(--space-sm) var(--space-md);
			min-height: 44px;
		}
		.sidebar-footer .btn {
			min-height: 44px;
			width: 100%;
			justify-content: flex-start;
		}
	}
</style>
