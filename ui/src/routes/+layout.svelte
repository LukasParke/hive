<script lang="ts">
	import '../app.css';
	import { authClient } from '$lib/auth-client';
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';

	let { children } = $props();

	const session = authClient.useSession();

	const navItems = [
		{ href: '/', label: 'Dashboard', icon: '⌂' },
		{ href: '/projects', label: 'Projects', icon: '□' },
		{ href: '/nodes', label: 'Nodes', icon: '◎' },
		{ href: '/catalog', label: 'Catalog', icon: '▦' },
		{ href: '/settings', label: 'Settings', icon: '⚙' },
	];

	function isActive(href: string): boolean {
		if (href === '/') return $page.url.pathname === '/';
		return $page.url.pathname.startsWith(href);
	}

	async function handleSignOut() {
		await authClient.signOut();
		goto('/auth/login');
	}

	$effect(() => {
		if (!$page.url.pathname.startsWith('/auth') && !$session.isPending && !$session.data) {
			goto('/auth/login');
		}
	});
</script>

{#if $page.url.pathname.startsWith('/auth')}
	{@render children()}
{:else if $session.isPending}
	<div class="flex h-screen items-center justify-center" style="background-color: var(--color-bg); color: var(--color-text-muted);">
		<p class="text-sm">Loading...</p>
	</div>
{:else if !$session.data}
	<div class="flex h-screen items-center justify-center" style="background-color: var(--color-bg); color: var(--color-text-muted);">
		<p class="text-sm">Redirecting to login...</p>
	</div>
{:else}
	<div class="flex h-screen">
		<aside class="w-60 flex flex-col border-r" style="background-color: var(--color-surface); border-color: var(--color-border);">
			<div class="p-4 border-b" style="border-color: var(--color-border);">
				<h1 class="text-xl font-bold" style="color: var(--color-primary);">Hive</h1>
				<p class="text-xs mt-1" style="color: var(--color-text-muted);">Swarm Orchestrator</p>
			</div>

			<nav class="flex-1 p-2 space-y-1">
				{#each navItems as item}
					<a
						href={item.href}
						class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors"
						style="color: {isActive(item.href) ? 'var(--color-text)' : 'var(--color-text-muted)'}; background-color: {isActive(item.href) ? 'var(--color-surface-hover)' : 'transparent'};"
					>
						<span class="text-base">{item.icon}</span>
						{item.label}
					</a>
				{/each}
			</nav>

			<div class="p-4 border-t" style="border-color: var(--color-border);">
				<div class="flex items-center justify-between">
					<div class="truncate">
						<p class="text-sm font-medium truncate">{$session.data.user.name || $session.data.user.email}</p>
					</div>
					<button onclick={handleSignOut} class="text-xs px-2 py-1 rounded cursor-pointer" style="color: var(--color-text-muted);">
						Sign out
					</button>
				</div>
			</div>
		</aside>

		<main class="flex-1 overflow-auto p-6">
			{@render children()}
		</main>
	</div>
{/if}
