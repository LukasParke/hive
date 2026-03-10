import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		proxy: {
			'/api/v1': 'http://localhost:9090',
			'/ws': {
				target: 'http://localhost:9090',
				ws: true,
			},
		},
	},
});
