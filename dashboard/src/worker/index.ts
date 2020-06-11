/// <reference path="./serviceworker.d.ts" />
/* eslint no-restricted-globals: 1 */
/* eslint-env serviceworker */

import * as types from './types';
import {
	load as loadConfig,
	setPrimaryClientID,
	getPrimaryClientID,
	setClientIDActive,
	unsetClientIDActive,
	getActiveClientIDs
} from './config';
import * as oauthClient from './oauthClient';
import { postMessageAll } from './external';
import _debug from './debug';

function debug(msg: string, ...args: any[]) {
	_debug(`[index]: ${msg}`, ...args);
}

self.addEventListener('install', (event: InstallEvent) => {
	debug('[oninstall] skipWaiting');
	self.skipWaiting();
});

// immediately claim all new clients
self.addEventListener('activate', function(event: ActivateEvent) {
	debug('[onactivate] claiming clients');
	event.waitUntil(self.clients.claim());
});

self.addEventListener('message', function(event: any) {
	const senderID = event.source.id;
	event.waitUntil(handleMessage(senderID, event.data as types.Message));
});

function handleMessage(senderID: string, message: types.Message): Promise<void> {
	if (message.type !== types.MessageType.PING) debug('handleMessage', message);
	setClientIDActive(senderID);
	return self.clients.matchAll().then(async (clientList: Client[]) => {
		const clientIDs = new Set<string>(clientList.map((c) => c.id));
		if (!clientIDs.has(senderID)) {
			// It's not clear why this is required, but on hard refreshes the
			// 'install' and 'activate' handles are never called
			debug(`[handleMessage]: sender (${senderID}) not in clients list, claiming all clients`);
			await self.clients.claim();
			debug('[handleMessage]: clients claimed, retrying');
			return handleMessage(senderID, message);
		}
		let primaryClientID = getPrimaryClientID();
		if (!primaryClientID || !clientIDs.has(primaryClientID)) {
			if (primaryClientID) unsetClientIDActive(primaryClientID);
			debug(`[handleMessage]: [${message.type}]: setPrimaryClientID(${senderID}) (from ${primaryClientID})`);
			primaryClientID = senderID;
			setPrimaryClientID(senderID);
		}

		clientList.forEach((client: Client) => {
			if (client.id !== senderID) return;
			switch (message.type) {
				case types.MessageType.CONFIG:
					loadConfig(message.payload);
					// make sure the client is authorized
					oauthClient.initClient(senderID);
					break;

				case types.MessageType.AUTH_ERROR:
					oauthClient.handleAuthError(message.payload);
					break;

				case types.MessageType.AUTH_CALLBACK:
					oauthClient.handleAuthorizationCallback(senderID, message.payload);
					break;

				case types.MessageType.RETRY_AUTH:
					oauthClient.initClient(senderID);
					break;

				case types.MessageType.PING:
					client.postMessage({
						type: types.MessageType.PONG,
						payload: getActiveClientIDs()
					});
					break;

				case types.MessageType.CLEAR_ERROR:
					// send the message back to all clients
					postMessageAll(message);
					break;

				default:
					// we don't know what this is, so send it back for debugging
					client.postMessage({
						type: types.MessageType.UNKNOWN,
						payload: message
					});
			}
		});
	});
}
