import ifDev from './ifDev';

export interface PublicConfig {
	CONTROLLER_HOST: string;
	OAUTH_ISSUER: string;
	OAUTH_CLIENT_ID: string;
}

export interface PrivateConfig {
	CONTROLLER_AUTH_KEY: string | null;
}

export interface Config extends PublicConfig, PrivateConfig {
	unsetPrivateConfig: () => void;
	isAuthenticated: () => boolean;
}

const config: Config = {
	CONTROLLER_HOST: process.env.CONTROLLER_HOST || ifDev(() => 'https://controller.1.localflynn.com') || '',
	CONTROLLER_AUTH_KEY: process.env.CONTROLLER_AUTH_KEY || null,

	OAUTH_ISSUER: process.env.OAUTH_ISSUER || '',
	OAUTH_CLIENT_ID: process.env.OAUTH_CLIENT_ID || '',

	unsetPrivateConfig: () => {
		config.CONTROLLER_AUTH_KEY = null;
	},

	isAuthenticated: () => {
		return config.CONTROLLER_AUTH_KEY !== null;
	}
};

export default config;
