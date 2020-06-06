/// <reference path="./serviceworker.d.ts" />
/* eslint no-restricted-globals: 1 */
/* eslint-env serviceworker */

import * as types from './types';
import { load as loadConfig, setPrimaryClientID } from './config';
import * as oauthClient from './oauthClient';

// self.addEventListener('install', (event: InstallEvent) => {
// 	self.skipWaiting();
// });

// immediately claim all new clients
self.addEventListener('activate', function(event: ActivateEvent) {
	event.waitUntil(self.clients.claim());
});

self.addEventListener('message', function(event: any) {
	const senderID = event.source.id;
	event.waitUntil(handleMessage(senderID, event.data as types.Message));
});

function handleMessage(senderID: string, message: types.Message): Promise<void> {
	return self.clients.matchAll().then((clientList: Client[]) => {
		clientList.forEach((client: Client) => {
			if (client.id !== senderID) return;
			switch (message.type) {
				case types.MessageType.CONFIG:
					setPrimaryClientID(senderID);
					if (loadConfig(message.payload) || clientList.length === 1) {
						// config has changes to any previously loaded config
						// OR this is the only client open
						oauthClient.init(senderID);
					}
					break;

				case types.MessageType.AUTH_CALLBACK:
					oauthClient.handleAuthorizationCallback(message.payload);
					break;

				case types.MessageType.PING:
					client.postMessage({
						type: types.MessageType.PONG
					});
					break;

				default:
					client.postMessage({
						type: types.MessageType.UNKNOWN,
						payload: message
					});
			}
		});
	});
}
