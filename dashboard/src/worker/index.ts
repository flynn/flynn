/// <reference path="./serviceworker.d.ts" />
/* eslint no-restricted-globals: 1 */
/* eslint-env serviceworker */

import * as types from './types';
import {
	load as loadConfig,
	hasActiveClientID,
	getPrimaryClientID,
	setClientIDActive,
	unsetClientIDActive,
	getActiveClientIDs
} from './config';
import * as oauthClient from './oauthClient';
import { postMessageAll } from './external';

self.addEventListener('install', (event: InstallEvent) => {
	self.skipWaiting();
});

// immediately claim all new clients
self.addEventListener('activate', function(event: ActivateEvent) {
	event.waitUntil(self.clients.claim());
});

self.addEventListener('message', function(event: any) {
	const senderID = event.source.id;
	event.waitUntil(handleMessage(senderID, event.data as types.Message));
});

function handleMessage(senderID: string, message: types.Message): Promise<void> {
	if (message.type !== types.MessageType.PING) console.log('[DEBUG]: SW handleMessage', message);
	if (!hasActiveClientID(senderID)) {
		// send token to new clients
		oauthClient.sendToken(senderID);
	}
	setClientIDActive(senderID);
	return self.clients.matchAll().then((clientList: Client[]) => {
		const clientIDs = new Set<string>(clientList.map((c) => c.id));
		let primaryClientID = getPrimaryClientID();
		if (!primaryClientID || !clientIDs.has(primaryClientID)) {
			if (primaryClientID) unsetClientIDActive(primaryClientID);
			primaryClientID = senderID;
		}

		clientList.forEach((client: Client) => {
			if (client.id !== senderID) return;
			switch (message.type) {
				case types.MessageType.CONFIG:
					loadConfig(message.payload);
					if (senderID === primaryClientID) {
						// config has changes to any previously loaded config
						// and this is coming from the primary client
						oauthClient.init(senderID);
					} else {
						// DEBUG:
						client.postMessage({
							type: types.MessageType.UNKNOWN,
							payload: {
								message: '[DEBUG]: aborted oauth config',
								primaryClientID,
								senderID,
								clientIDs
							} as any
						});
					}
					break;

				case types.MessageType.AUTH_ERROR:
					oauthClient.handleAuthError(message.payload);
					break;

				case types.MessageType.AUTH_CALLBACK:
					oauthClient.handleAuthorizationCallback(message.payload);
					break;

				case types.MessageType.RETRY_AUTH:
					oauthClient.init(senderID);
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
