import ifDev from './ifDev';

export interface PublicConfig {
	CONTROLLER_HOST: string;
	OAUTH_ISSUER: string;
	OAUTH_CLIENT_ID: string;
	PUBLIC_URL: string;
	WORKER_URL: string;
}

export interface PrivateConfig {
	CONTROLLER_AUTH_KEY: string | null;
}

type AuthCallback = (authenticated: boolean) => void;

export interface Config extends PublicConfig, PrivateConfig {
	unsetPrivateConfig: () => void;
	setAuthKey: (key: string | null) => void;
	authCallback: (cb: AuthCallback) => () => void;
	isAuthenticated: () => boolean;
}

const authCallbacks = new Set<AuthCallback>();

const config: Config = {
	CONTROLLER_HOST: process.env.CONTROLLER_HOST || ifDev(() => 'https://controller.1.localflynn.com') || '',
	CONTROLLER_AUTH_KEY: process.env.CONTROLLER_AUTH_KEY || null,

	OAUTH_ISSUER: process.env.OAUTH_ISSUER || '',
	OAUTH_CLIENT_ID: process.env.OAUTH_CLIENT_ID || '',

	PUBLIC_URL: process.env.PUBLIC_URL || '',
	WORKER_URL: process.env.WORKER_URL || '',

	unsetPrivateConfig: () => {
		config.CONTROLLER_AUTH_KEY = null;
	},

	setAuthKey: (key: string | null) => {
		config.CONTROLLER_AUTH_KEY = key;

		const isAuthenticated = config.isAuthenticated();
		authCallbacks.forEach((cb) => {
			cb(isAuthenticated);
		});
	},

	authCallback: (cb: AuthCallback) => {
		authCallbacks.add(cb);
		return () => {
			authCallbacks.delete(cb);
		};
	},

	isAuthenticated: () => {
		return config.CONTROLLER_AUTH_KEY !== null;
	}
};

export default config;
