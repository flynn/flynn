import ifDev from './ifDev';
import * as types from './worker/types';
import { isTokenValid } from './worker/tokenHelpers';

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
type AuthErrorCallback = (error: Error) => void;
type CancelFunc = () => void;

export interface Config extends PublicConfig, PrivateConfig {
	AUTH_TOKEN: types.OAuthToken | null;
	unsetPrivateConfig: () => void;
	setAuth: (token: types.OAuthToken | null) => void;
	authCallback: (fn: AuthCallback) => CancelFunc;
	isAuthenticated: () => boolean;
	authErrorCallback: (fn: AuthErrorCallback) => CancelFunc;
	handleAuthError: (error: Error) => void;
}

const authCallbacks = new Set<AuthCallback>();
const authErrorCallbacks = new Set<AuthErrorCallback>();

const config: Config = {
	CONTROLLER_HOST: process.env.CONTROLLER_HOST || ifDev(() => 'https://controller.1.localflynn.com') || '',
	CONTROLLER_AUTH_KEY: process.env.CONTROLLER_AUTH_KEY || null,

	OAUTH_ISSUER: process.env.OAUTH_ISSUER || '',
	OAUTH_CLIENT_ID: process.env.OAUTH_CLIENT_ID || '',

	PUBLIC_URL: process.env.PUBLIC_URL || '',
	WORKER_URL: process.env.WORKER_URL || '',

	AUTH_TOKEN: null,

	unsetPrivateConfig: () => {
		config.CONTROLLER_AUTH_KEY = null;
	},

	setAuth: (token: types.OAuthToken | null) => {
		config.AUTH_TOKEN = token;

		const isAuthenticated = config.isAuthenticated();
		authCallbacks.forEach((fn) => {
			fn(isAuthenticated);
		});
	},

	authCallback: (fn: AuthCallback) => {
		authCallbacks.add(fn);
		return () => {
			authCallbacks.delete(fn);
		};
	},

	isAuthenticated: () => {
		return isTokenValid(config.AUTH_TOKEN);
	},

	authErrorCallback: (fn: AuthErrorCallback) => {
		authErrorCallbacks.add(fn);
		return () => {
			authErrorCallbacks.delete(fn);
		};
	},

	handleAuthError: (error: Error) => {
		authErrorCallbacks.forEach((fn) => fn(error));
		if (authErrorCallbacks.size === 0) {
			throw error;
		}
	}
};

export default config;
