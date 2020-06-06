const IDBName = 'OAuth';
const IDBVersion = 1;
const IDBStoreNames = {
	SERVER_META: 'servermeta',
	RESPONSE_PARAMS: 'responseparams',
	TOKENS: 'tokens',
	CACHE: 'cache'
};
let database: IDBDatabase | null = null;

async function getDatabase(): Promise<IDBDatabase> {
	if (database) {
		return Promise.resolve(database);
	}

	let resolve: (db: IDBDatabase) => void;
	let reject: (error: Error) => void;
	const p = new Promise((rs: (db: IDBDatabase) => void, rj: (error: Error) => void) => {
		resolve = rs;
		reject = rj;
	});

	const request = indexedDB.open(IDBName, IDBVersion);
	request.addEventListener('error', function() {
		reject(request.error || new Error('Error opening IndexedDB database'));
	});

	// upgradeneeded is fired when the database is first created or when a new
	// version is created
	request.addEventListener('upgradeneeded', function(event: IDBVersionChangeEvent) {
		const db = request.result;

		// setup the database
		const serverMetaStore = db.createObjectStore(IDBStoreNames.SERVER_META, { keyPath: 'issuer' });
		serverMetaStore.createIndex('issuer', 'issuer', { unique: true });
		serverMetaStore.createIndex('authorization_endpoint', 'authorization_endpoint', { unique: false });
		serverMetaStore.createIndex('token_endpoint', 'token_endpoint', { unique: false });
		serverMetaStore.createIndex('token_endpoint_auth_methods_supported', 'token_endpoint_auth_methods_supported', {
			unique: false
		});
		serverMetaStore.createIndex(
			'token_endpoint_auth_signing_alg_values_supported',
			'token_endpoint_auth_signing_alg_values_supported',
			{ unique: false }
		);
		serverMetaStore.createIndex('userinfo_endpoint', 'userinfo_endpoint', { unique: false });

		const responseParamsStore = db.createObjectStore(IDBStoreNames.RESPONSE_PARAMS, { keyPath: 'ssn' });
		responseParamsStore.createIndex('state', 'state', { unique: true });
		responseParamsStore.createIndex('code', 'code', { unique: true });
		responseParamsStore.createIndex('error', 'error', { unique: false });
		responseParamsStore.createIndex('error_description', 'error_description', { unique: false });

		const tokensStore = db.createObjectStore(IDBStoreNames.TOKENS, { keyPath: 'issuer' });
		tokensStore.createIndex('issuer', 'issuer', { unique: true });
		tokensStore.createIndex('access_token', 'acess_token', { unique: true });
		tokensStore.createIndex('refresh_token', 'refresh_token', { unique: true });
		tokensStore.createIndex('expires_in', 'expires_in', { unique: false });
		tokensStore.createIndex('refresh_token_expires_in', 'refresh_token_expires_in', { unique: false });
		tokensStore.createIndex('issued_time', 'issued_time', { unique: false });
		tokensStore.createIndex('token_type', 'token_type', { unique: false });

		const cacheStore = db.createObjectStore(IDBStoreNames.CACHE, { keyPath: 'state' });
		cacheStore.createIndex('state', 'state', { unique: true });
		cacheStore.createIndex('code_verifier', 'code_verifier', { unique: false });
		cacheStore.createIndex('code_challenge', 'code_challenge', { unique: false });
		cacheStore.createIndex('redirect_uri', 'redirect_uri', { unique: false });

		// save a referece scoped to this module
		database = db;
		// let our caller know the database is ready
		resolve(db);
	});

	return p;
}

async function dbGet(storeName: string, lookupKey: string): Promise<any> {
	let resolve: (result: any) => void;
	let reject: (error: Error) => void;
	const p = new Promise((rs: (result: any) => void, rj: (error: Error) => void) => {
		resolve = rs;
		reject = rj;
	});

	let result: any;
	const db = await getDatabase();
	const transaction = db.transaction([storeName], 'readonly');
	transaction.addEventListener('error', () => {
		reject(transaction.error || new Error('Unknown IDBTransaction Error'));
	});
	transaction.addEventListener('abort', () => {
		reject(new Error('IDBTransaction aborted'));
	});
	transaction.addEventListener('complete', () => {
		resolve(result);
	});

	transaction.objectStore(storeName).get(lookupKey).onsuccess = (event: any) => {
		result = event.target.result;
	};

	return p;
}

async function dbSet(storeName: string, row: any, key?: string): Promise<any> {
	let resolve: () => void;
	let reject: (error: Error) => void;
	const p = new Promise((rs: () => void, rj: (error: Error) => void) => {
		resolve = rs;
		reject = rj;
	});

	const db = await getDatabase();
	const transaction = db.transaction([storeName], 'readwrite');
	transaction.addEventListener('error', () => {
		reject(transaction.error || new Error('Unknown IDBTransaction Error'));
	});
	transaction.addEventListener('abort', () => {
		reject(new Error('IDBTransaction aborted'));
	});
	transaction.addEventListener('complete', () => {
		resolve();
	});

	const objectStore = transaction.objectStore(storeName);
	objectStore.add(row, key);

	return p;
}
