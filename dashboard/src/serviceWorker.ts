import runtime from 'serviceworker-webpack-plugin/lib/runtime';
import * as types from './worker/types';
import Config from './config';

function resolveRelativeURI(path: string): string {
	const a = document.createElement('a');
	a.href = path;
	return a.href;
}

function init(): () => void {
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
		try {
			postMessage({
				type: types.MessageType.PING
			});
		} catch (e) {
			console.error(e);
		}
	}, 1000);

	return () => {
		clearInterval(intervalID);
	};
}

export async function register(): Promise<void> {
	let promiseComplete = false;
	return new Promise((resolve: () => void, reject: (error: Error) => void) => {
		if ('serviceWorker' in navigator) {
			if (navigator.serviceWorker.controller) {
				var url = navigator.serviceWorker.controller.scriptURL;
				console.log('[DEBUG]: serviceWorker.controller', url);

				if (!promiseComplete) {
					promiseComplete = true;
					resolve();
				}

				// TODO(jvatic): do we need this?
				navigator.serviceWorker.ready.then(function(registration) {
					registration.update();
				});
			} else {
				runtime.register({ scope: '/' });
			}

			let teardown: () => void;

			navigator.serviceWorker.ready.then(function(registration) {
				console.log('[DEBUG]: ServiceWorker ready', !!navigator.serviceWorker.controller);
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
				console.log('[DEBUG]: ServiceWorker controllerchange');
				if (navigator.serviceWorker.controller) {
					var scriptURL = navigator.serviceWorker.controller.scriptURL;
					console.log('[DEBUG]: serviceWorker.onControllerchange', scriptURL);
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

function handleMessage(message: types.Message) {
	switch (message.type) {
		case types.MessageType.PONG:
			break;

		case types.MessageType.AUTH_REQUEST:
			window.open(message.payload);
			break;

		case types.MessageType.AUTH_TOKEN:
			Config.setAuthKey(message.payload.access_token);
			break;

		case types.MessageType.ERROR:
			console.error(message.payload);
			break;

		default:
			console.log('ServiceWorker message', message);
	}
}

export function postMessage(message: types.Message) {
	if (!navigator.serviceWorker.controller) throw new Error('postMessage: No ServiceWorker found');
	navigator.serviceWorker.controller.postMessage(message);
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
