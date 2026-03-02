<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, type SwarmNode, type NodeAllDisks, type BlockDevice } from '$lib/api';

	let step = $state(1);
	let error = $state('');
	let deploying = $state(false);

	// Step 1: Config
	let clusterName = $state('hive-ceph');
	let replicationSize = $state(3);
	let createCephFS = $state(true);
	let cephFSName = $state('hive-fs');

	// Step 2: Node selection
	let nodes = $state<SwarmNode[]>([]);
	let selectedMonNodes = $state<Set<string>>(new Set());

	// Step 3: Disk selection
	let nodeDisks = $state<NodeAllDisks[]>([]);
	let selectedDisks = $state<Map<string, Set<string>>>(new Map());
	let loadingDisks = $state(false);

	onMount(async () => {
		try {
			const data = await api.listNodes();
			nodes = data.nodes ?? [];
		} catch (e: any) {
			error = e.message;
		}
	});

	function toggleMonNode(nodeId: string) {
		const next = new Set(selectedMonNodes);
		if (next.has(nodeId)) {
			next.delete(nodeId);
		} else {
			next.add(nodeId);
		}
		selectedMonNodes = next;
	}

	async function loadDisks() {
		loadingDisks = true;
		try {
			nodeDisks = await api.discoverAllDisks();
		} catch (e: any) {
			error = e.message;
		} finally {
			loadingDisks = false;
		}
	}

	function toggleDisk(nodeId: string, devicePath: string) {
		const next = new Map(selectedDisks);
		if (!next.has(nodeId)) {
			next.set(nodeId, new Set());
		}
		const disks = new Set(next.get(nodeId)!);
		if (disks.has(devicePath)) {
			disks.delete(devicePath);
		} else {
			disks.add(devicePath);
		}
		next.set(nodeId, disks);
		selectedDisks = next;
	}

	function isDiskSelected(nodeId: string, devicePath: string): boolean {
		return selectedDisks.get(nodeId)?.has(devicePath) ?? false;
	}

	function formatBytes(bytes: number): string {
		if (!bytes || bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
	}

	function getNodeHostname(nodeId: string): string {
		const node = nodes.find(n => n.id === nodeId);
		return node?.hostname || nodeId;
	}

	function getNodeIP(nodeId: string): string {
		const node = nodes.find(n => n.id === nodeId);
		return node?.addr?.replace(':2377', '') || '';
	}

	function totalSelectedDisks(): number {
		let count = 0;
		for (const disks of selectedDisks.values()) {
			count += disks.size;
		}
		return count;
	}

	async function goToStep(s: number) {
		if (s === 3 && nodeDisks.length === 0) {
			await loadDisks();
		}
		step = s;
	}

	async function deploy() {
		deploying = true;
		error = '';
		try {
			const monNodes = Array.from(selectedMonNodes).map(nodeId => ({
				node_id: nodeId,
				hostname: getNodeHostname(nodeId),
				ip: getNodeIP(nodeId),
			}));

			const osdSelections: { node_id: string; hostname: string; device_path: string; device_size?: number; device_type?: string }[] = [];
			for (const [nodeId, disks] of selectedDisks) {
				const nodeDisk = nodeDisks.find(nd => nd.node_id === nodeId);
				for (const devicePath of disks) {
					const bd = nodeDisk?.block_devices.find((d: BlockDevice) => d.path === devicePath);
					osdSelections.push({
						node_id: nodeId,
						hostname: getNodeHostname(nodeId),
						device_path: devicePath,
						device_size: bd?.size,
						device_type: bd?.rotational ? 'hdd' : 'ssd',
					});
				}
			}

			const bootstrapNodeId = monNodes[0]?.node_id;

			await api.createCephCluster({
				name: clusterName,
				bootstrap_node_id: bootstrapNodeId,
				mon_nodes: monNodes,
				osd_selections: osdSelections,
				replication_size: replicationSize,
				create_cephfs: createCephFS,
				cephfs_name: cephFSName,
			});

			goto('/storage/ceph');
		} catch (e: any) {
			error = e.message;
		} finally {
			deploying = false;
		}
	}
</script>

<div>
	<div class="flex items-center gap-4 mb-6">
		<a href="/storage/ceph" style="color: var(--color-muted); text-decoration: none;">← Back</a>
		<h2 class="text-2xl font-bold">Deploy Ceph Cluster</h2>
	</div>

	<!-- Step indicators -->
	<div class="flex gap-2 mb-8">
		{#each [
			{ n: 1, label: 'Configure' },
			{ n: 2, label: 'Select Nodes' },
			{ n: 3, label: 'Select Disks' },
			{ n: 4, label: 'Review & Deploy' }
		] as s}
			<button
				onclick={() => goToStep(s.n)}
				class="px-4 py-2 rounded-lg text-sm font-medium cursor-pointer"
				style={step === s.n
					? 'background-color: var(--color-primary); color: var(--color-bg);'
					: 'background-color: var(--color-surface); color: var(--color-muted); border: 1px solid var(--color-border);'}
			>
				{s.n}. {s.label}
			</button>
		{/each}
	</div>

	{#if error}
		<div class="rounded-lg p-4 mb-4" style="background-color: rgba(239, 68, 68, 0.1); border: 1px solid var(--color-danger);">
			<p style="color: var(--color-danger);">{error}</p>
		</div>
	{/if}

	<!-- Step 1: Configure -->
	{#if step === 1}
		<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-4">Cluster Configuration</h3>
			<div class="space-y-4 max-w-md">
				<div>
					<label class="block text-sm font-medium mb-1">Cluster Name</label>
					<input
						type="text"
						bind:value={clusterName}
						class="w-full px-3 py-2 rounded-lg text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					/>
				</div>
				<div>
					<label class="block text-sm font-medium mb-1">Replication Size</label>
					<select
						bind:value={replicationSize}
						class="w-full px-3 py-2 rounded-lg text-sm"
						style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
					>
						<option value={1}>1 (no replication)</option>
						<option value={2}>2 (2x replication)</option>
						<option value={3}>3 (3x replication - recommended)</option>
					</select>
				</div>
				<div class="flex items-center gap-3">
					<input type="checkbox" bind:checked={createCephFS} id="cephfs" />
					<label for="cephfs" class="text-sm">Create CephFS filesystem</label>
				</div>
				{#if createCephFS}
					<div>
						<label class="block text-sm font-medium mb-1">CephFS Name</label>
						<input
							type="text"
							bind:value={cephFSName}
							class="w-full px-3 py-2 rounded-lg text-sm"
							style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
						/>
					</div>
				{/if}
			</div>
			<div class="mt-6">
				<button
					onclick={() => goToStep(2)}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-primary); color: var(--color-bg);"
				>
					Next: Select Nodes
				</button>
			</div>
		</div>
	{/if}

	<!-- Step 2: Select Nodes -->
	{#if step === 2}
		<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-2">Select Monitor Nodes</h3>
			<p class="text-sm mb-4" style="color: var(--color-muted);">
				Select {replicationSize >= 3 ? '3 or more' : 'at least 1'} nodes for Ceph monitors. For HA, use an odd number (3 or 5).
			</p>

			{#if nodes.length === 0}
				<p style="color: var(--color-muted);">No nodes available.</p>
			{:else}
				<div class="space-y-2">
					{#each nodes as node}
						<button
							onclick={() => toggleMonNode(node.id)}
							class="w-full text-left p-4 rounded-lg cursor-pointer"
							style={selectedMonNodes.has(node.id)
								? 'background-color: rgba(var(--color-primary-rgb, 59, 130, 246), 0.1); border: 2px solid var(--color-primary);'
								: 'background-color: var(--color-bg); border: 1px solid var(--color-border);'}
						>
							<div class="flex items-center justify-between">
								<div>
									<span class="font-medium">{node.hostname}</span>
									<span class="text-sm ml-2" style="color: var(--color-muted);">{node.addr}</span>
								</div>
								<div class="flex items-center gap-3">
									<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-muted);">
										{node.role}
									</span>
									{#if selectedMonNodes.has(node.id)}
										<span style="color: var(--color-primary);">Selected</span>
									{/if}
								</div>
							</div>
						</button>
					{/each}
				</div>
			{/if}

			<div class="mt-6 flex gap-3">
				<button
					onclick={() => goToStep(1)}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				>
					Back
				</button>
				<button
					onclick={() => goToStep(3)}
					disabled={selectedMonNodes.size === 0}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-primary); color: var(--color-bg); opacity: {selectedMonNodes.size === 0 ? 0.5 : 1};"
				>
					Next: Select Disks ({selectedMonNodes.size} nodes selected)
				</button>
			</div>
		</div>
	{/if}

	<!-- Step 3: Select Disks -->
	{#if step === 3}
		<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-2">Select Storage Disks</h3>
			<p class="text-sm mb-4" style="color: var(--color-muted);">
				Select raw block devices to use as OSDs. Only available (unmounted, unformatted) disks are shown.
			</p>

			{#if loadingDisks}
				<p style="color: var(--color-muted);">Discovering disks across nodes...</p>
			{:else}
				{#each nodeDisks as nd}
					<div class="mb-4">
						<h4 class="font-medium mb-2">{nd.hostname} <span class="text-sm" style="color: var(--color-muted);">({nd.node_id})</span></h4>
						{#if nd.block_devices.filter((d: BlockDevice) => d.available).length === 0}
							<p class="text-sm ml-4" style="color: var(--color-muted);">No available disks on this node.</p>
						{:else}
							<div class="space-y-1 ml-4">
								{#each nd.block_devices.filter((d: BlockDevice) => d.available) as disk}
									<button
										onclick={() => toggleDisk(nd.node_id, disk.path)}
										class="w-full text-left p-3 rounded-lg cursor-pointer"
										style={isDiskSelected(nd.node_id, disk.path)
											? 'background-color: rgba(var(--color-primary-rgb, 59, 130, 246), 0.1); border: 2px solid var(--color-primary);'
											: 'background-color: var(--color-bg); border: 1px solid var(--color-border);'}
									>
										<div class="flex items-center justify-between">
											<div class="flex items-center gap-4">
												<span class="font-mono text-sm">{disk.path}</span>
												<span class="text-sm font-medium">{formatBytes(disk.size)}</span>
												<span class="text-xs px-2 py-0.5 rounded" style="background-color: var(--color-bg); color: var(--color-muted);">
													{disk.rotational ? 'HDD' : 'SSD/NVMe'}
												</span>
												{#if disk.model}
													<span class="text-xs" style="color: var(--color-muted);">{disk.model}</span>
												{/if}
											</div>
											{#if isDiskSelected(nd.node_id, disk.path)}
												<span style="color: var(--color-primary);">Selected</span>
											{/if}
										</div>
									</button>
								{/each}
							</div>
						{/if}
					</div>
				{/each}
			{/if}

			<div class="mt-6 flex gap-3">
				<button
					onclick={() => goToStep(2)}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				>
					Back
				</button>
				<button
					onclick={() => goToStep(4)}
					disabled={totalSelectedDisks() === 0}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-primary); color: var(--color-bg); opacity: {totalSelectedDisks() === 0 ? 0.5 : 1};"
				>
					Next: Review ({totalSelectedDisks()} disks selected)
				</button>
			</div>
		</div>
	{/if}

	<!-- Step 4: Review & Deploy -->
	{#if step === 4}
		<div class="rounded-lg p-6" style="background-color: var(--color-surface); border: 1px solid var(--color-border);">
			<h3 class="text-lg font-semibold mb-4">Review & Deploy</h3>

			<div class="space-y-4">
				<div class="grid grid-cols-2 gap-4 text-sm">
					<div>
						<span style="color: var(--color-muted);">Cluster Name</span>
						<p class="font-medium">{clusterName}</p>
					</div>
					<div>
						<span style="color: var(--color-muted);">Replication</span>
						<p class="font-medium">{replicationSize}x</p>
					</div>
					<div>
						<span style="color: var(--color-muted);">Monitor Nodes</span>
						<p class="font-medium">{selectedMonNodes.size}</p>
					</div>
					<div>
						<span style="color: var(--color-muted);">Total OSDs</span>
						<p class="font-medium">{totalSelectedDisks()}</p>
					</div>
					<div>
						<span style="color: var(--color-muted);">CephFS</span>
						<p class="font-medium">{createCephFS ? cephFSName : 'Not creating'}</p>
					</div>
				</div>

				<div class="pt-4" style="border-top: 1px solid var(--color-border);">
					<h4 class="font-medium mb-2 text-sm">Monitor Nodes</h4>
					<div class="space-y-1">
						{#each Array.from(selectedMonNodes) as nodeId}
							<p class="text-sm font-mono">{getNodeHostname(nodeId)} ({getNodeIP(nodeId)})</p>
						{/each}
					</div>
				</div>

				<div class="pt-4" style="border-top: 1px solid var(--color-border);">
					<h4 class="font-medium mb-2 text-sm">OSD Disks</h4>
					{#each Array.from(selectedDisks.entries()) as [nodeId, disks]}
						<div class="mb-2">
							<p class="text-sm font-medium">{getNodeHostname(nodeId)}</p>
							{#each Array.from(disks) as disk}
								<p class="text-sm font-mono ml-4">{disk}</p>
							{/each}
						</div>
					{/each}
				</div>
			</div>

			<div class="mt-6 flex gap-3">
				<button
					onclick={() => goToStep(3)}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-bg); border: 1px solid var(--color-border); color: var(--color-text);"
				>
					Back
				</button>
				<button
					onclick={deploy}
					disabled={deploying}
					class="px-6 py-2 rounded-lg text-sm font-medium cursor-pointer"
					style="background-color: var(--color-primary); color: var(--color-bg); opacity: {deploying ? 0.5 : 1};"
				>
					{deploying ? 'Deploying...' : 'Deploy Ceph Cluster'}
				</button>
			</div>
		</div>
	{/if}
</div>
