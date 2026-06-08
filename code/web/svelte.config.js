import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// Build a single-page app: a static index.html plus assets. The Go
		// server serves these as the website, and Capacitor bundles the same
		// output into the iOS/Android apps.
		adapter: adapter({ fallback: 'index.html' }),
		alias: { $lib: 'src/lib' }
	}
};

export default config;
