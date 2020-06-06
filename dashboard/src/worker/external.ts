/// <reference path="./serviceworker.d.ts" />
/* eslint no-restricted-globals: 1 */
/* eslint-env serviceworker */

import * as types from './types';

export async function getClients(): Promise<Client[]> {
	return self.clients.matchAll();
}

async function getClient(clientID: string): Promise<Client | null> {
	const clients = await getClients();
	let client = clients.find((c) => c.id === clientID) || null;
	return client;
}

export async function postMessageAll(message: types.Message) {
	const clients = await getClients();
	clients.forEach((client) => {
		client.postMessage(message);
	});
}

export async function postMessage(clientID: string, message: types.Message) {
	const client = await getClient(clientID);
	if (!client) throw new Error(`postMessage: Error: Client(${clientID}) available`);
	client.postMessage(message);
}
