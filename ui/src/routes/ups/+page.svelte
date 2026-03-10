<script lang="ts">
	import { api } from '$lib/api';
	import { invalidateAll } from '$app/navigation';
	import type { UPSDevice, UPSStatusSnapshot } from '$lib/types';
	import { GaugeRing } from '$lib/components';

	type UPSWithStatus = { device: UPSDevice; status?: UPSStatusSnapshot };

	let { data } = $props();

	let showForm = $state(false);
	let saving = $state(false);
	let error = $state<string | null>(null);

	let form = $state({
		name: '',
		nut_host: '',
		nut_port: 3493,
		ups_name: 'ups',
		poll_interval_seconds: 30,
		shutdown_threshold: 20,
	});

	function resetForm() {
		form = {
			name: '',
			nut_host: '',
			nut_port: 3493,
			ups_name: 'ups',
			poll_interval_seconds: 30,
			shutdown_threshold: 20,
		};
		showForm = false;
		error = null;
	}

	async function submitForm() {
		saving = true;
		error = null;
		try {
			await api.createUPS({
				name: form.name,
				nut_host: form.nut_host,
				nut_port: form.nut_port,
				ups_name: form.ups_name,
				poll_interval_seconds: form.poll_interval_seconds,
				shutdown_threshold: form.shutdown_threshold,
			});
			resetForm();
			await invalidateAll();
		} catch (e: unknown) {
			error = e instanceof Error ? e.message : 'Failed to add device';
		} finally {
			saving = false;
		}
	}

	async function deleteDevice(id: string) {
		if (!confirm('Remove this UPS device? Status history will be lost.')) return;
		try {
			await api.deleteUPS(id);
			await invalidateAll();
		} catch (e: unknown) {
			alert(e instanceof Error ? e.message : 'Failed to delete');
		}
	}

	function formatDate(d: string) {
		return new Date(d).toLocaleString();
	}

	function statusBadgeClass(status: string | undefined): string {
		if (!status) return 'bg-slate-600 text-slate-400';
		if (status === 'OL' || status === 'online') return 'bg-emerald-500/20 text-emerald-400';
		if (status === 'OB' || status === 'on battery') return 'bg-amber-500/20 text-amber-400';
		return 'bg-slate-600 text-slate-400';
	}

	const devices = $derived(
		((data?.data ?? []) as UPSWithStatus[]).filter((x) => x?.device)
	);
</script>

<svelte:head><title>UPS | Hive</title></svelte:head>

<div class="max-w-5xl mx-auto">
	<div class="page-header">
		<div>
			<h2 class="page-title">UPS</h2>
			<p class="page-subtitle">Monitor UPS devices via NUT (Network UPS Tools)</p>
		</div>
		<button
			class="btn btn-primary"
			onclick={() => (showForm ? resetForm() : (showForm = true))}
		>
			{showForm ? 'Cancel' : '+ Add Device'}
		</button>
	</div>

	{#if showForm}
		<form
			onsubmit={(e) => {
				e.preventDefault();
				submitForm();
			}}
			class="rounded-lg p-6 mb-6 bg-slate-800/50 border border-slate-700 space-y-4"
		>
			<h3 class="text-lg font-semibold text-slate-200">Add UPS Device</h3>
			{#if error}
				<div class="text-sm text-red-400 bg-red-900/20 px-3 py-2 rounded">
					{error}
				</div>
			{/if}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="ups-name">Name</label>
					<input id="ups-name" type="text" bind:value={form.name} required placeholder="apc-main" />
				</div>
				<div>
					<label for="ups-host">NUT Host</label>
					<input id="ups-host" type="text" bind:value={form.nut_host} required placeholder="192.168.1.50" />
				</div>
				<div>
					<label for="ups-port">Port</label>
					<input id="ups-port" type="number" bind:value={form.nut_port} min="1" max="65535" />
				</div>
				<div>
					<label for="ups-upsname">UPS Name</label>
					<input id="ups-upsname" type="text" bind:value={form.ups_name} placeholder="ups" />
				</div>
				<div>
					<label for="ups-poll">Poll Interval (seconds)</label>
					<input id="ups-poll" type="number" bind:value={form.poll_interval_seconds} min="5" />
				</div>
				<div>
					<label for="ups-threshold">Shutdown Threshold (%)</label>
					<input id="ups-threshold" type="number" bind:value={form.shutdown_threshold} min="1" max="100" />
				</div>
			</div>
			<div class="flex justify-end gap-3">
				<button type="button" class="btn bg-slate-700 text-slate-200 border-slate-600" onclick={resetForm}>
					Cancel
				</button>
				<button type="submit" class="btn btn-primary" disabled={saving}>
					{saving ? 'Adding...' : 'Add Device'}
				</button>
			</div>
		</form>
	{/if}

	{#if devices.length === 0 && !showForm}
		<div class="rounded-lg p-8 text-center bg-slate-800/50 border border-slate-700">
			<p class="text-lg font-medium text-slate-200 mb-2">No UPS devices configured</p>
			<p class="text-sm text-slate-400 mb-4">Add a NUT-compatible UPS to monitor battery, load, and trigger safe shutdowns.</p>
			<button class="btn btn-primary" onclick={() => (showForm = true)}>
				+ Add Your First Device
			</button>
		</div>
	{:else}
		<div class="space-y-4">
			{#each devices as { device, status } (device.id)}
				<div class="rounded-lg p-5 bg-slate-800/50 border border-slate-700">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-3 mb-3 flex-wrap">
								<h3 class="font-semibold text-lg text-slate-200">{device.name}</h3>
								<span
									class="text-xs px-2 py-0.5 rounded-full font-medium {statusBadgeClass(status?.status)}"
								>
									{status?.status ?? 'No data'}
								</span>
								<span class="text-xs text-slate-500 font-mono">{device.nut_host}:{device.nut_port} / {device.ups_name}</span>
							</div>

							{#if status}
								<div class="flex flex-wrap items-center gap-6">
									<div class="flex items-center gap-2">
										<GaugeRing value={status.battery_charge ?? 0} size={40} />
										<span class="text-sm text-slate-400">Battery {Math.round(status.battery_charge ?? 0)}%</span>
									</div>
									<div class="text-sm text-slate-400">
										Load: <span class="text-slate-300 font-medium">{Math.round(status.load_percent ?? 0)}%</span>
									</div>
									{#if (status.input_voltage ?? 0) > 0}
										<div class="text-sm text-slate-400">
											In: <span class="text-slate-300">{Math.round(status.input_voltage ?? 0)}V</span>
										</div>
									{/if}
									{#if (status.output_voltage ?? 0) > 0}
										<div class="text-sm text-slate-400">
											Out: <span class="text-slate-300">{Math.round(status.output_voltage ?? 0)}V</span>
										</div>
									{/if}
									{#if (status.battery_runtime ?? 0) > 0}
										<div class="text-sm text-slate-400">
											Runtime: <span class="text-slate-300">{Math.round((status.battery_runtime ?? 0) / 60)} min</span>
										</div>
									{/if}
									<span class="text-xs text-slate-500">{formatDate(status.timestamp)}</span>
								</div>
							{:else}
								<p class="text-sm text-slate-500">Waiting for status data...</p>
							{/if}

							<p class="text-xs text-slate-500 mt-2">
								Poll every {device.poll_interval_seconds}s · Shutdown at {device.shutdown_threshold ?? 20}%
							</p>
						</div>
						<button
							class="btn bg-red-900/40 text-red-400 border-red-800 hover:bg-red-900/60"
							onclick={() => deleteDevice(device.id)}
						>
							Delete
						</button>
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
