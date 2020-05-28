import Config from './config';

enum StoreKeys {
	SERVER_META = 'OAUTH_META',
	CODE_VERIFIER = 'OAUTH_CODE_VERIFIER',
	CODE_CHALLENGE = 'OAUTH_CODE_CHALLENGE',
	STATE = 'OAUTH_STATE',
	REDIRECT_URI = 'OAUTH_REDIRECT_URI',
	TOKEN = 'OAUTH_TOKEN',
	ORIGINAL_PATH = 'OAUTH_ORIGINAL_PATH'
}

const Store = {
	setItem: async function setItem(key: StoreKeys[keyof StoreKeys], value: string): Promise<boolean> {
		localStorage.setItem(key.toString(), value);
		return true;
	},

	getItem: async function setItem(key: StoreKeys[keyof StoreKeys]): Promise<string> {
		return localStorage.getItem(key.toString()) || '';
	},

	removeItem: async function removeItem(key: StoreKeys[keyof StoreKeys]): Promise<boolean> {
		localStorage.removeItem(key.toString());
		return true;
	},

	clear: async function clear(): Promise<boolean> {
		await Object.values(StoreKeys).forEach(async (key: string) => {
			await Store.removeItem(key.toString());
		});
		return true;
	},

	setToken: async function setToken(token: Token | null): Promise<boolean> {
		if (token === null) {
			return Store.removeItem(StoreKeys.TOKEN);
		}
		return Store.setItem(StoreKeys.TOKEN, JSON.stringify(token));
	},

	getToken: async function getToken(): Promise<Token | null> {
		let token: Token | null;
		try {
			token = JSON.parse(await Store.getItem(StoreKeys.TOKEN));
		} catch (e) {
			token = null;
		}
		return token;
	}
};

export const OAUTH_CALLBACK_PATH = '/oauth/callback';

function resolveURLPath(path: string): string {
	const a = document.createElement('a');
	a.href = path;
	return a.href;
}

export async function generateAuthorizationURL(): Promise<string> {
	const meta = await getServerMeta();
	const params = new URLSearchParams('');
	params.set('code_challenge', await generateCodeChallenge());
	params.set('code_challenge_method', 'S256');
	params.set('state', await generateState());
	params.set('nonce', await randomString(16));
	params.set('client_id', Config.OAUTH_CLIENT_ID);
	params.set('response_type', 'code');
	params.set('response_mode', 'fragment');
	await Store.setItem(StoreKeys.ORIGINAL_PATH, window.location.pathname);
	const redirectURI = resolveURLPath(OAUTH_CALLBACK_PATH);
	await Store.setItem(StoreKeys.REDIRECT_URI, redirectURI);
	params.set('redirect_uri', redirectURI);
	return `${meta.authorization_endpoint}?${params.toString()}`;
}

export async function getOriginalPath(): Promise<string> {
	return await Store.getItem(StoreKeys.ORIGINAL_PATH);
}

export interface Token {
	access_token: string;
	token_type: string;
	expires_in: number;
	refresh_token: string;
	refresh_token_expires_in: number;

	issued_time: number;
}

type TokenCallbackFn = (token: Token | null, error: Error | null) => void;

export async function getToken(callback: TokenCallbackFn) {
	const token = await Store.getToken();
	if (token === null || token.issued_time + token.expires_in * 1000 <= Date.now()) {
		if (token && token.issued_time + token.refresh_token_expires_in > Date.now()) {
			await refreshToken(token.refresh_token);
			getToken(callback);
			return;
		}
		callback(null, new Error('token not found'));
	} else {
		callback(token, null);
	}
}

export async function tokenExchange(responseParams: string, callback: TokenCallbackFn, abortSignal: AbortSignal) {
	const params = new URLSearchParams(responseParams);
	if (!(await verifyState(params.get('state') || ''))) {
		throw new Error(`Error verifying state param`);
	}

	if (params.get('error')) {
		const errorCode = params.get('error') || '';
		const error = Object.assign(new Error(`Error: ${params.get('error_description') || errorCode}`), {
			code: errorCode
		});
		throw error;
	}

	const meta = await getServerMeta();
	const body = new URLSearchParams();
	body.set('grant_type', 'authorization_code');
	body.set('code', decodeURIComponent(params.get('code') || ''));
	body.set('code_verifier', await Store.getItem(StoreKeys.CODE_VERIFIER));
	body.set('redirect_uri', await Store.getItem(StoreKeys.REDIRECT_URI));
	body.set('client_id', Config.OAUTH_CLIENT_ID);
	const res = await fetch(meta.token_endpoint, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/x-www-form-urlencoded'
		},
		body: body.toString(),
		signal: abortSignal
	});
	const token = await res.json();
	if (abortSignal.aborted) return;
	if (!token.error) {
		token.issued_time = Date.now();
		token.expires_in = 30;
		refreshTokenTimeout = setTimeout(() => {
			refreshToken(token.refresh_token || '');
		}, (token.expires_in - 30) * 1000);
		await Store.setToken(token);
		await Store.removeItem(StoreKeys.CODE_VERIFIER);
		await Store.removeItem(StoreKeys.CODE_CHALLENGE);
		await Store.removeItem(StoreKeys.STATE);
		await Store.removeItem(StoreKeys.REDIRECT_URI);
		await Store.removeItem(StoreKeys.ORIGINAL_PATH);
		Config.setAuthKey(token.access_token || null);
		callback(token as Token, null);
	} else {
		await Store.setToken(null);
		Config.setAuthKey(null);
		callback(null, new Error(`Error getting auth token: ${token.error_description || token.error}`));
	}
}

let refreshTokenTimeout: ReturnType<typeof setTimeout>;

async function refreshToken(code: string) {
	clearTimeout(refreshTokenTimeout);
	const meta = await getServerMeta();
	const params = new URLSearchParams('');
	params.set('grant_type', 'refresh_token');
	params.set('code', code);
	const res = await fetch(meta.token_endpoint, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/x-www-form-urlencoded'
		},
		body: params.toString()
	}).catch(async (e) => {
		await Store.clear();
		throw e;
	});
	const token = await res.json();
	if (!token.error) {
		token.issued_time = Date.now();
		refreshTokenTimeout = setTimeout(() => {
			refreshToken(token.refresh_token || '');
		}, (token.expires_in - 30) * 1000);
		await Store.setToken(token);
		Config.setAuthKey(token.access_token);
	} else {
		Config.setAuthKey(null);
		await Store.setToken(null);
		console.error(new Error(`Error refreshing auth token: ${token.error_description || token.error}`));
	}
}

interface ServerMetadata {
	issuer: string;
	authorization_endpoint: string;
	token_endpoint: string;
	token_endpoint_auth_methods_supported: string;
	token_endpoint_auth_signing_alg_values_supported: string;
	userinfo_endpoint: string;
}

async function getServerMeta(): Promise<ServerMetadata> {
	const localMetaJSON = await Store.getItem(StoreKeys.SERVER_META);
	if (localMetaJSON !== '') {
		const localMeta = JSON.parse(localMetaJSON);
		if (codePointCompare(Config.OAUTH_ISSUER, localMeta.issuer)) {
			return localMeta;
		}
	}

	const url = `${Config.OAUTH_ISSUER}/.well-known/oauth-authorization-server`;
	const res = await fetch(url);
	const meta = await res.json();
	if (!codePointCompare(Config.OAUTH_ISSUER, meta.issuer)) {
		throw new Error(
			`Error verifying OAuth Server Metadata: Issuer mismatch: "${Config.OAUTH_ISSUER}" != ${meta.issuer}`
		);
	}
	return meta;
}

function codePointCompare(a: string, b: string): boolean {
	if (a.length !== b.length) return false;
	for (let len = a.length, i = 0; i < len; i++) {
		if (a.codePointAt(i) !== b.codePointAt(i)) return false;
	}
	return true;
}

async function base64(input: ArrayBuffer): Promise<string> {
	let resolve: (str: string) => void;
	let reject: (error: Error) => void;
	const p = new Promise<string>((rs, rj) => {
		resolve = rs;
		reject = rj;
	});

	const blob = new Blob([input], { type: 'application/octet-binary' });
	var reader = new FileReader();
	reader.addEventListener('error', (e) => {
		reader.abort();
		reject(reader.error as Error);
	});
	reader.addEventListener('load', (e) => {
		const res = (reader.result || '') as string;
		// trim data:*/*;base64, prefix
		const prefix = 'base64,';
		resolve(res.slice(res.indexOf(prefix) + prefix.length).replace(/=+$/, ''));
	});

	reader.readAsDataURL(blob);

	return p;
}

async function sha256(input: string): Promise<ArrayBuffer> {
	const encoder = new TextEncoder();
	const data = encoder.encode(input);
	return crypto.subtle.digest('SHA-256', data);
}

function randomString(length: number): string {
	const randomValues = random(length * 2);
	return hex(randomValues).slice(0, length);
}

function random(length: number): ArrayBuffer {
	const buffer = new ArrayBuffer(length);
	const array = new Uint32Array(buffer);
	crypto.getRandomValues(array);
	return buffer;
}

function hex(input: ArrayBuffer): string {
	const view = new Int32Array(input);
	return Array.from(view, function(b) {
		return ('0' + (b & 0xff).toString(16)).slice(-2);
	}).join('');
}

async function generateCodeChallenge(): Promise<string> {
	const codeVerifier = randomString(64);
	await Store.setItem(StoreKeys.CODE_VERIFIER, codeVerifier);
	const codeChallenge = await base64(await sha256(codeVerifier));
	await Store.setItem(StoreKeys.CODE_CHALLENGE, codeChallenge);
	return codeChallenge;
}

async function generateState(): Promise<string> {
	const state = randomString(16);
	await Store.setItem(StoreKeys.STATE, state);
	return state;
}

async function verifyState(state: string): Promise<boolean> {
	return codePointCompare(await Store.getItem(StoreKeys.STATE), state);
}
