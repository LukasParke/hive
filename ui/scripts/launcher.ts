import { runLauncher } from '../src/lib/server/bootstrap/launcher.js';

const isManaged = process.env.HIVE_MANAGED === 'true';
const dataDir = process.env.HIVE_DATA_DIR || '/data';

if (!isManaged) {
	console.log('[hive] Running in launcher mode...');
	runLauncher({ dataDir })
		.then(() => {
			console.log('[hive] Launcher complete, exiting.');
			process.exit(0);
		})
		.catch((err) => {
			console.error('[hive] Launcher failed:', err);
			process.exit(1);
		});
} else {
	console.log('[hive] HIVE_MANAGED=true, skipping launcher, starting SvelteKit server...');
	import('../build/index.js');
}
