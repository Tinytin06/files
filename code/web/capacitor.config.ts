import type { CapacitorConfig } from '@capacitor/cli';

// Wraps the built Svelte SPA (in ./build) into native iOS/Android apps.
// The mobile apps are static shells, so they must talk to the API over the
// network: set VITE_API_BASE_URL to your deployed https URL before building
// for mobile (e.g. VITE_API_BASE_URL=https://cryptex.example.com/api npm run build).
const config: CapacitorConfig = {
	appId: 'com.example.cryptex',
	appName: 'Cryptex',
	webDir: 'build'
};

export default config;
