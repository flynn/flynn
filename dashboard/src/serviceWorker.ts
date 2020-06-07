import runtime from 'serviceworker-webpack-plugin/lib/runtime';
import * as types from './worker/types';
import Config from './config';
import { handleErrorFactory, ErrorHandler, CancelFunc } from './useErrorHandler';
import _debug from './debug';

function debug(msg: string, ...args: any[]) {
	_debug(`[serviceWorker]: ${msg}`, ...args);
}

function resolveRelativeURI(path: string): string {
	const a = document.createElement('a');
	a.href = path;
	return a.href;
}

let handleError: ErrorHandler;

function init(): () => void {
	debug('init');
	handleError = handleErrorFactory();

	const cancelAuthErrorCallback = Config.authErrorCallback((error: any) => {
		error = Object.assign({}, error, { message: error.message });
		// delete possible functions from the error object so it can be sent
		delete error.retry;
		delete error.cancel;
		postMessage({
			type: types.MessageType.AUTH_ERROR,
			payload: error
		});
	});

	postMessage({
		type: types.MessageType.CONFIG,
		payload: {
			CONTROLLER_HOST: Config.CONTROLLER_HOST,
			OAUTH_ISSUER: Config.OAUTH_ISSUER,
			OAUTH_CLIENT_ID: Config.OAUTH_CLIENT_ID,
			OAUTH_CALLBACK_URI: resolveRelativeURI('/oauth/callback')
		}
	});

	// ping the service worker every second to make sure it stays active
	const intervalID = setInterval(() => {
		if (
			!postMessage({
				type: types.MessageType.PING
			})
		) {
			clearInterval(intervalID);
		}
	}, 1000);

	return () => {
		debug('clear init state');
		clearInterval(intervalID);
		cancelAuthErrorCallback();
	};
}

export async function register() {
	let promiseComplete = false;
	return new Promise((resolve: () => void, reject: (error: Error) => void) => {
		if ('serviceWorker' in navigator) {
			if (navigator.serviceWorker.controller) {
				var url = navigator.serviceWorker.controller.scriptURL;
				debug('controller', url);

				if (!promiseComplete) {
					promiseComplete = true;
					resolve();
				}

				// TODO(jvatic): do we need this?
				// navigator.serviceWorker.ready.then(function(registration) {
				// 	registration.update();
				// });
			} else {
				runtime.register({ scope: '/' });
			}

			let teardown: () => void;

			navigator.serviceWorker.ready.then(function(registration) {
				debug('ready', !!navigator.serviceWorker.controller);
				if (navigator.serviceWorker.controller) {
					if (teardown) teardown();
					teardown = init();
					if (!promiseComplete) {
						promiseComplete = true;
						resolve();
					}
				}
			});

			navigator.serviceWorker.addEventListener('controllerchange', function() {
				debug('controllerchange');
				if (navigator.serviceWorker.controller) {
					var scriptURL = navigator.serviceWorker.controller.scriptURL;
					debug('controllerchange', scriptURL);
					if (teardown) teardown();
					teardown = init();
					if (!promiseComplete) {
						promiseComplete = true;
						resolve();
					}
				}
			});

			navigator.serviceWorker.addEventListener('message', function(event: MessageEvent) {
				handleMessage(event.data as types.Message);
			});
		} else {
			reject(new Error('ServiceWorker required and not available.'));
		}
	});
}

let messageCallbacks = new Map<string, Array<(message: types.Message) => void>>();

export function addEventListener(type: string, callback: (message: types.Message) => void): () => void {
	messageCallbacks.set(type, (messageCallbacks.get(type) || []).concat(callback));
	return () => {
		const fns = messageCallbacks.get(type) || [];
		const index = fns.indexOf(callback);
		if (index !== -1) {
			messageCallbacks.set(type, fns.slice(0, index).concat(fns.slice(index + 1)));
		}
	};
}

const errorsMap = new Map<string, CancelFunc>();

function handleMessage(message: types.Message) {
	switch (message.type) {
		case types.MessageType.PONG:
			// TODO: setup a death switch here for the worker (if it doesn't pong
			// back when we ping we should assume it's dead and show an error message
			// prompting a page reload)
			break;

		case types.MessageType.AUTH_REQUEST:
			window.open(message.payload);
			break;

		case types.MessageType.AUTH_TOKEN:
			Config.setAuthKey(message.payload.access_token);
			break;

		case types.MessageType.AUTH_ERROR:
			const authError = message.payload;
			const cancelAuthError = handleError(
				Object.assign(new Error(authError.message), {
					retry: () => {
						postMessage({
							type: types.MessageType.CLEAR_ERROR,
							payload: (authError as any).id
						});
						postMessage({
							type: types.MessageType.RETRY_AUTH
						});
					}
				})
			);
			if ((authError as any).id) {
				errorsMap.set((authError as any).id, cancelAuthError);
			}
			debug('AUTH_ERROR', authError);
			break;

		case types.MessageType.ERROR:
			const error = message.payload;
			const cancelError = handleError(
				Object.assign(new Error(error.message), {
					cancel: () => {
						postMessage({
							type: types.MessageType.CLEAR_ERROR,
							payload: (error as any).id
						});
					}
				})
			);
			if ((error as any).id) {
				errorsMap.set((error as any).id, cancelError);
			}
			debug('ERROR', error);
			break;

		case types.MessageType.CLEAR_ERROR:
			console.log('clear error', message.payload);
			const cancelFn = errorsMap.get(message.payload);
			if (cancelFn) cancelFn();
			break;

		default:
			console.log('message', message);
	}

	(messageCallbacks.get(message.type) || []).forEach((fn) => fn(message));
}

export function postMessage(message: types.Message) {
	if (!navigator.serviceWorker.controller) {
		handleError(
			Object.assign(new Error('postMessage: No ServiceWorker found'), {
				retry: () => {
					window.location.reload();
				}
			})
		);
		return false;
	}
	navigator.serviceWorker.controller.postMessage(message);
	return true;
}

// type WorkerConfig = {
// 	onSuccess?: (registration: ServiceWorkerRegistration) => void;
// 	onUpdate?: (registration: ServiceWorkerRegistration) => void;
// };

export function unregister() {
	if ('serviceWorker' in navigator) {
		navigator.serviceWorker.ready.then((registration) => {
			registration.unregister();
		});
	}
}
