import ifDev from './ifDev';

export interface PublicConfig {
	CONTROLLER_HOST: string;
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

	unsetPrivateConfig: () => {
		config.CONTROLLER_AUTH_KEY = null;
	},

	isAuthenticated: () => {
		return config.CONTROLLER_AUTH_KEY !== null;
	}
};

export default config;
