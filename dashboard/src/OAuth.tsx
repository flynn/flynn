import * as React from 'react';

import * as oauth from './oauth-client';
import Config from './config';
import useErrorHandler from './useErrorHandler';
import useRouter from './useRouter';

function getOrigin(): string {
	return `${window.location.protocol}//${window.location.host}`;
}

export default function OAuth() {
	const handleError = useErrorHandler();
	const { history } = useRouter();
	const [authenticated, setAuthenticated] = React.useState(false);
	React.useEffect(() => {
		// oauth-client calls Config.setAuthKey
		const cancel = Config.authCallback((authenticated) => {
			setAuthenticated(authenticated);
		});
		return cancel;
	}, []);

	let isAuthWindow = false;
	if (window.location.pathname === '/oauth/callback') {
		const parent = window.opener;
		if (parent) {
			isAuthWindow = true;
			parent.postMessage(
				{
					action: 'callback',
					payload: window.location.hash.substr(1)
				},
				getOrigin()
			);
			window.close();
		}
	}

	React.useEffect(() => {
		if (isAuthWindow) return () => {};

		const ac = new AbortController();
		const receiveMessage = (event: MessageEvent) => {
			if (authenticated) return;
			if (event.origin !== getOrigin()) return;
			if (event.data.action !== 'callback') return;

			const oauthParams = event.data.payload;
			console.log(oauthParams);
			oauth
				.tokenExchange(
					oauthParams,
					(token: oauth.Token | null, error: Error | null) => {
						if (ac.signal.aborted === true) return;
						if (error !== null) {
							handleError(error);
						}
					},
					ac.signal
				)
				.catch((error) => {
					if (ac.signal.aborted === true) return;
					handleError(error);
					console.error(error);
				});
		};
		window.addEventListener('message', receiveMessage, false);
		const cancel = () => {
			ac.abort();
			window.removeEventListener('message', receiveMessage);
		};
		return cancel;
	});

	React.useEffect(() => {
		if (authenticated || isAuthWindow) return () => {};

		let authWindow: ReturnType<typeof window.open>;

		const ac = new AbortController();
		const cancel = () => {
			ac.abort();
			if (authWindow) authWindow.close();
		};

		oauth.getToken((token: oauth.Token | null, error: Error | null) => {
			if (ac.signal.aborted === true) return;
			setAuthenticated(token !== null);
			if (token === null) {
				oauth
					.generateAuthorizationURL()
					.then((url: string) => {
						if (ac.signal.aborted === true) return;
						authWindow = window.open(url, 'OAuth', '');
					})
					.catch((error) => {
						if (ac.signal.aborted === true) return;
						console.error(error);
						handleError(new Error(`Error authenticating: ${error}`));
					});
			}
		});
		return cancel;
	}, [authenticated, handleError, history, isAuthWindow]);

	return null;
}
