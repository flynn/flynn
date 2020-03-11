import * as React from 'react';
import * as oauth from './oauth';
import Config from './config';
import useErrorHandler from './useErrorHandler';
import useRouter from './useRouter';

export default function useOAuth() {
	const handleError = useErrorHandler();
	const { history } = useRouter();
	const [authenticated, setAuthenticated] = React.useState(false);
	React.useEffect(() => {
		const cancel = Config.authCallback((authenticated) => {
			setAuthenticated(authenticated);
		});
		return cancel;
	}, []);

	React.useEffect(() => {
		const ac = new AbortController();
		const cancel = () => {
			ac.abort();
		};
		if (window.location.pathname === '/oauth/callback') {
			oauth
				.tokenExchange(
					window.location.hash.substr(1),
					(token: oauth.Token | null, error: Error | null) => {
						if (ac.signal.aborted === true) return;
						if (error !== null) {
							handleError(error);
						}

						if (token !== null) {
							const params = new URLSearchParams(window.location.search);
							const path = params.get('path') || '/';
							params.delete('path');
							history.replace(`${path}?${params.toString()}`);
						}
					},
					ac.signal
				)
				.catch((error) => {
					if (ac.signal.aborted === true) return;
					console.error(error);
					handleError(error);
				});
			return cancel;
		}

		oauth.getToken((token: oauth.Token | null, error: Error | null) => {
			if (ac.signal.aborted === true) return;
			setAuthenticated(token !== null);
			if (token === null) {
				oauth
					.generateAuthorizationURL()
					.then((url: string) => {
						if (ac.signal.aborted === true) return;
						window.location.href = url;
					})
					.catch((error) => {
						if (ac.signal.aborted === true) return;
						console.error(error);
						handleError(new Error(`Error authenticating: ${error}`));
					});
			}
		});
		return cancel;
	}, [handleError, history]);
	return { authenticated };
}
