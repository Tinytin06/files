import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		// During `npm run dev`, proxy API calls to the local Go server so the
		// browser hits one origin (no CORS) just like in production.
		proxy: {
			'/api': 'http://localhost:8080'
		}
	}
});
