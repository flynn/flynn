/// <reference path="./serviceworker.d.ts" />
/* eslint no-restricted-globals: 1 */
/* eslint-env serviceworker */

import { get as dbGet, set as dbSet, del as dbDel } from 'idb-keyval';

import * as types from './types';
import { getConfig } from './config';
import { encode as base64URLEncode } from '../util/base64url';
import { postMessageAll, postMessage } from './external';

const DBKeys = {
	SERVER_META: 'servermeta',
	AUTHORIZATION_CACHE: 'cache',
	CALLBACK_RESPONSE: 'callbackresponse',
	TOKEN: 'token'
};

function dbClearAuth(): Promise<any> {
	return Promise.all(
		Object.values(DBKeys).map((dbKey) => {
			return dbDel(dbKey);
		})
	);
}

export async function init(clientID: string) {
	const token = await getToken();
	if (isTokenValid(token)) {
		await postMessageAll({
			type: types.MessageType.AUTH_TOKEN,
			payload: token as types.OAuthToken
		});
		return;
	} else if (canRefreshToken(token)) {
		try {
			await doTokenRefresh((token as types.OAuthToken).refresh_token);
		} catch (error) {
			await handleError(types.MessageType.AUTH_ERROR, error);
		}
	} else {
		// start fresh
		await dbClearAuth();

		const url = await generateAuthorizationURL();
		await postMessage(clientID, {
			type: types.MessageType.AUTH_REQUEST,
			payload: url
		});
	}
}

export async function handleAuthError(error: Error) {
	// send the error to all clients where it will be displayed with the ability
	// to retry
	await handleError(types.MessageType.AUTH_ERROR, error);
	// clear cached auth data
	await dbClearAuth();
}

export async function handleAuthorizationCallback(queryString: string): Promise<void> {
	console.log('[DEBUG]: handleAuthorizationCallback', queryString);
	try {
		const params = new URLSearchParams(queryString);
		const res: types.OAuthCallbackResponse = {
			state: params.get('state') || null,
			code: params.get('code') || null,
			error: params.get('error') || null,
			error_description: params.get('error_description') || null
		};
		await dbSet(DBKeys.CALLBACK_RESPONSE, res);
		await doTokenExchange(res);
	} catch (error) {
		await handleError(types.MessageType.AUTH_ERROR, error);
		await dbClearAuth();
	}
	return;
}

let refreshTokenTimeout: ReturnType<typeof setTimeout>;

// TODO(jvatic): figure out why TypeScript was giving me issue with this
// type: types.MessageType.AUTH_ERROR | types.MessageType.ERROR
async function handleError(type: any, error: Error) {
	// stop the refresh token cycle
	clearTimeout(refreshTokenTimeout);

	// add an ID to errors so they can be cleared for all clients
	const errorID = (error as any).id ? ((error as any).id as string) : await randomString(16);
	await postMessageAll({
		type,
		payload: Object.assign({ message: error.message }, error, { id: errorID })
	});
}

async function setToken(token: types.OAuthToken) {
	clearTimeout(refreshTokenTimeout);

	await postMessageAll({
		type: types.MessageType.AUTH_TOKEN,
		payload: token
	});
	await dbSet(DBKeys.TOKEN, token);

	if (canRefreshToken(token)) {
		const refreshTokenExpiresMs = token.refresh_token_expires_in * 1000;
		const refreshDelayMs = refreshTokenExpiresMs - 10000; // refresh 10s before expires
		refreshTokenTimeout = setTimeout(async () => {
			try {
				await doTokenRefresh(token.refresh_token);
			} catch (error) {
				await handleError(types.MessageType.AUTH_ERROR, error);
				await dbClearAuth();
			}
		}, refreshDelayMs);
	}
}

async function getToken(): Promise<types.OAuthToken | null> {
	try {
		const token = (await dbGet(DBKeys.TOKEN)) || null;
		return token as types.OAuthToken | null;
	} catch (e) {
		return null;
	}
}

function isTokenValid(token: types.OAuthToken | null): boolean {
	if (token === null) return false;
	if (token.issued_time + token.expires_in * 1000 >= Date.now()) {
		return true;
	}
	return false;
}

function canRefreshToken(token: types.OAuthToken | null): boolean {
	if (token === null) return false;
	if (token && token.issued_time + token.refresh_token_expires_in > Date.now()) {
		return true;
	}
	return false;
}

interface ServerMetadata {
	issuer: string;
	authorization_endpoint: string;
	token_endpoint: string;
	token_endpoint_auth_methods_supported: string;
	token_endpoint_auth_signing_alg_values_supported: string;
	userinfo_endpoint: string;
}

function codePointCompare(a: string, b: string): boolean {
	if (a.length !== b.length) return false;
	for (let len = a.length, i = 0; i < len; i++) {
		if (a.codePointAt(i) !== b.codePointAt(i)) return false;
	}
	return true;
}

async function getServerMeta(): Promise<ServerMetadata> {
	const config = await getConfig();
	const cachedServerMeta = ((await dbGet(DBKeys.SERVER_META)) as ServerMetadata) || null;
	if (cachedServerMeta) {
		if (codePointCompare(config.OAUTH_ISSUER, cachedServerMeta.issuer)) {
			return cachedServerMeta;
		}
	}

	const url = `${config.OAUTH_ISSUER}/.well-known/oauth-authorization-server`;
	const res = await fetch(url);
	const meta = await res.json();
	if (!codePointCompare(config.OAUTH_ISSUER, meta.issuer)) {
		throw new Error(
			`Error verifying OAuth Server Metadata: Issuer mismatch: "${config.OAUTH_ISSUER}" != ${meta.issuer}`
		);
	}
	await dbSet(DBKeys.SERVER_META, meta);
	return meta;
}

async function base64(input: ArrayBuffer): Promise<string> {
	return Promise.resolve(base64URLEncode(String.fromCharCode(...new Uint8Array(input))));
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

async function generateCodeChallenge(): Promise<[string, string]> {
	const codeVerifier = randomString(64);
	const codeChallenge = await base64(await sha256(codeVerifier));
	return [codeVerifier, codeChallenge];
}

async function generateState(): Promise<string> {
	const state = randomString(16);
	return state;
}

async function generateAuthorizationURL(): Promise<string> {
	const config = await getConfig();
	const meta = await getServerMeta();
	const params = new URLSearchParams('');
	const [codeVerifier, codeChallenge] = await generateCodeChallenge();
	const state = await generateState();
	params.set('code_challenge', codeChallenge);
	params.set('code_challenge_method', 'S256');
	params.set('state', state);
	params.set('nonce', await randomString(16));
	params.set('client_id', config.OAUTH_CLIENT_ID);
	params.set('response_type', 'code');
	params.set('response_mode', 'query');
	const redirectURI = config.OAUTH_CALLBACK_URI;
	params.set('redirect_uri', redirectURI);

	await dbSet(DBKeys.AUTHORIZATION_CACHE, {
		codeVerifier,
		codeChallenge,
		redirectURI,
		state
	});

	return `${meta.authorization_endpoint}?${params.toString()}`;
}

interface TokenError {
	error: string;
	error_description: string;
}

function buildError(error: TokenError, message = ''): Error {
	return Object.assign(new Error(`${message ? message + ': ' : ''}${error.error_description || error.error}`), {
		code: error.error,
		description: error.error_description
	});
}

async function doTokenExchange(params: types.OAuthCallbackResponse) {
	const cachedValues = ((await dbGet(DBKeys.AUTHORIZATION_CACHE)) as types.OAuthCachedValues) || null;
	if (!cachedValues) {
		throw new Error('doTokenExchange: Error: corrupt data');
	}

	if (!(await codePointCompare(params.state || '', cachedValues.state))) {
		throw new Error(`Error verifying state param`);
	}

	if (params.error) {
		const errorCode = params.error || '';
		const error = Object.assign(new Error(`Error: ${params.error_description || errorCode}`), {
			code: errorCode
		});
		throw error;
	}

	const meta = await getServerMeta();
	const config = await getConfig();

	// clear cached values from auth redirect
	await dbDel(DBKeys.AUTHORIZATION_CACHE);
	await dbDel(DBKeys.CALLBACK_RESPONSE);

	const body = new URLSearchParams();
	body.set('grant_type', 'authorization_code');
	body.set('code', decodeURIComponent(params.code || ''));
	body.set('code_verifier', cachedValues.codeVerifier);
	// body.set('code_verifier', 'foo');
	body.set('redirect_uri', cachedValues.redirectURI);
	body.set('client_id', config.OAUTH_CLIENT_ID);
	body.set('audience', config.CONTROLLER_HOST);
	const res = await fetch(meta.token_endpoint, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/x-www-form-urlencoded'
		},
		body: body.toString()
	});
	const token = await res.json();
	if (!token.error) {
		token.issued_time = Date.now();
		await setToken(token as types.OAuthToken);
	} else {
		throw buildError(token as TokenError, 'Error getting auth token');
	}
}

async function doTokenRefresh(refreshToken: string) {
	const config = await getConfig();
	const meta = await getServerMeta();
	const body = new URLSearchParams('');
	body.set('grant_type', 'refresh_token');
	body.set('refresh_token', refreshToken);
	body.set('client_id', config.OAUTH_CLIENT_ID);
	body.set('audience', config.CONTROLLER_HOST);
	const res = await fetch(meta.token_endpoint, {
		method: 'POST',
		headers: {
			'Content-Type': 'application/x-www-form-urlencoded'
		},
		body: body.toString()
	});
	const token = await res.json();
	if (!token.error) {
		token.issued_time = Date.now();
		await setToken(token as types.OAuthToken);
	} else {
		throw buildError(token, 'Error refreshing auth token');
	}
}
