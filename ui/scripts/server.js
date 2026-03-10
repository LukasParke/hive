import { handler } from './build/handler.js';
import { createServer } from 'http';
import { WebSocketServer, WebSocket } from 'ws';

const PORT = parseInt(process.env.PORT || '8080', 10);
const ENGINE_URL = process.env.HIVE_ENGINE_URL || 'http://hive-engine:9090';
const ENGINE_WS = ENGINE_URL.replace(/^http/, 'ws');

const server = createServer((req, res) => {
	handler(req, res, () => {
		res.writeHead(404).end('Not found');
	});
});

server.on('upgrade', (req, socket, head) => {
	if (!req.url?.startsWith('/ws/')) {
		socket.destroy();
		return;
	}

	const targetUrl = `${ENGINE_WS}${req.url}`;
	let upgraded = false;

	const target = new WebSocket(targetUrl, {
		headers: { cookie: req.headers.cookie || '' },
	});

	target.on('error', () => {
		if (!upgraded) {
			// Engine unreachable — reject the upgrade with HTTP 503
			try {
				socket.write(
					'HTTP/1.1 503 Service Unavailable\r\n' +
					'Content-Type: text/plain\r\n' +
					'Connection: close\r\n' +
					'\r\n' +
					'Engine unavailable'
				);
			} catch { /* socket may already be dead */ }
			socket.destroy();
		}
	});

	target.on('open', () => {
		upgraded = true;
		const wss = new WebSocketServer({ noServer: true });
		wss.handleUpgrade(req, socket, head, (client) => {
			client.on('message', (data, isBinary) => {
				if (target.readyState === WebSocket.OPEN) {
					target.send(data, { binary: isBinary });
				}
			});
			target.on('message', (data, isBinary) => {
				if (client.readyState === WebSocket.OPEN) {
					client.send(data, { binary: isBinary });
				}
			});
			client.on('close', () => target.close());
			target.on('close', () => client.close());
			client.on('error', () => client.close());
		});
	});
});

server.listen(PORT, '0.0.0.0', () => {
	console.log(`Listening on http://0.0.0.0:${PORT}`);
});
