<script lang="ts">
	import { onMount, onDestroy } from 'svelte';

	let {
		execId = '',
		onDisconnect = () => {}
	}: {
		execId: string;
		onDisconnect?: () => void;
	} = $props();

	let terminalEl: HTMLDivElement;
	let term: any;
	let fitAddon: any;
	let ws: WebSocket | null = null;

	onMount(async () => {
		const { Terminal } = await import('@xterm/xterm');
		const { FitAddon } = await import('@xterm/addon-fit');
		await import('@xterm/xterm/css/xterm.css');

		fitAddon = new FitAddon();
		term = new Terminal({
			cursorBlink: true,
			fontSize: 14,
			fontFamily: 'JetBrains Mono, Menlo, Monaco, Consolas, monospace',
			theme: {
				background: '#0f172a',
				foreground: '#e2e8f0',
				cursor: '#f59e0b',
				selectionBackground: '#334155',
			}
		});

		term.loadAddon(fitAddon);
		term.open(terminalEl);
		fitAddon.fit();

		const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
		ws = new WebSocket(`${proto}//${location.host}/ws/exec/${execId}`);
		ws.binaryType = 'arraybuffer';

		ws.onopen = () => {
			const dims = { type: 'resize', cols: term.cols, rows: term.rows };
			ws!.send(JSON.stringify(dims));
		};

		ws.onmessage = (e) => {
			if (e.data instanceof ArrayBuffer) {
				term.write(new Uint8Array(e.data));
			} else {
				term.write(e.data);
			}
		};

		ws.onclose = () => {
			term.write('\r\n\x1b[33m[Session ended]\x1b[0m\r\n');
			onDisconnect();
		};

		term.onData((data: string) => {
			if (ws && ws.readyState === WebSocket.OPEN) {
				ws.send(data);
			}
		});

		term.onResize(({ cols, rows }: { cols: number; rows: number }) => {
			if (ws && ws.readyState === WebSocket.OPEN) {
				ws.send(JSON.stringify({ type: 'resize', cols, rows }));
			}
		});

		const resizeObs = new ResizeObserver(() => fitAddon?.fit());
		resizeObs.observe(terminalEl);

		return () => resizeObs.disconnect();
	});

	onDestroy(() => {
		ws?.close();
		term?.dispose();
	});
</script>

<div bind:this={terminalEl} class="w-full h-full min-h-[400px] rounded-lg overflow-hidden"></div>
