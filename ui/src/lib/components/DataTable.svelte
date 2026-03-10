<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';

	interface Column<T> {
		key: string;
		label: string;
		class?: string;
	}

	interface Props {
		columns: Column<T>[];
		rows: T[];
		emptyMessage?: string;
		row: Snippet<[T, number]>;
	}

	let {
		columns,
		rows,
		emptyMessage = 'No data found',
		row,
	}: Props = $props();
</script>

{#if rows.length === 0}
	<div class="empty-state">
		<svg class="empty-state-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
			<rect x="3" y="3" width="18" height="18" rx="2" />
			<path d="M3 9h18M9 21V9" />
		</svg>
		<p>{emptyMessage}</p>
	</div>
{:else}
	<div style="overflow-x: auto">
		<table class="hive-table">
			<thead>
				<tr>
					{#each columns as col}
						<th class={col.class ?? ''}>{col.label}</th>
					{/each}
				</tr>
			</thead>
			<tbody>
				{#each rows as item, i}
					<tr>
						{@render row(item, i)}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
