import * as React from 'react';

import * as oauth from './oauth-client';
import Config from './config';
import useErrorHandler from './useErrorHandler';
import RightOverlay from './RightOverlay';

function getOrigin(): string {
	return `${window.location.protocol}//${window.location.host}`;
}

let attempts = 0;
const maxAttempts = 3;

enum ActionType {
	AUTHORIZATION_CALLBACK = 'OAuth__AUTHORIZATION_CALLBACK',
	AUTHORIZED = 'OAuth__AUTHORIZED',
	ERROR = 'OAuth__ERROR',
	RESET = 'OAuth__RESET'
}

interface AuthorizationCallbackAction {
	type: ActionType.AUTHORIZATION_CALLBACK;
	params: string;
}

interface AuthorizedAction {
	type: ActionType.AUTHORIZED;
}

interface ErrorAction {
	type: ActionType.ERROR;
	error: Error;
}

interface ResetAction {
	type: ActionType.RESET;
}

type Action = AuthorizationCallbackAction | AuthorizedAction | ErrorAction | ResetAction;

enum Steps {
	UNAUTHORIZED = 1,
	AUTHORIZATION_CALLBACK,
	TOKEN_EXCHANGE,
	AUTHORIZED,
	ERROR
}

interface State {
	step: Steps;
	token: oauth.Token | null;
	authorizationResponseParams: oauth.AuthorizationResponseParams | null;
	error: Error | null;
}

function reducer(prevState: State, action: Action): State {
	const nextState = (() => {
		const nextState = Object.assign({}, prevState);

		switch (action.type) {
			case ActionType.AUTHORIZATION_CALLBACK:
				const params = new URLSearchParams(action.params);
				nextState.authorizationResponseParams = {
					state: params.get('state') || null,
					code: params.get('code') || null,
					error: params.get('error') || null,
					error_description: params.get('error_description') || null
				};
				nextState.step = Steps.TOKEN_EXCHANGE;
				return nextState;

			case ActionType.AUTHORIZED:
				nextState.step = Steps.AUTHORIZED;
				return nextState;

			case ActionType.ERROR:
				nextState.error = action.error;
				nextState.step = Steps.ERROR;
				return nextState;

			case ActionType.RESET:
				// reset auth flow to the start
				attempts = 0;
				nextState.step = Steps.UNAUTHORIZED;
				return nextState;

			default:
				throw new Error(`Unknown action type: ${(action as any).type}`);
		}
	})();

	if (nextState === prevState) return prevState;

	return nextState;
}

function initialState(): State {
	let step;
	if (window.location.pathname === '/oauth/callback') {
		step = Steps.AUTHORIZATION_CALLBACK;
	} else {
		step = Steps.UNAUTHORIZED;
	}
	return {
		step,
		token: null,
		authorizationResponseParams: null,
		error: null
	};
}

export default function OAuth() {
	const handleError = useErrorHandler();
	const [{ step, authorizationResponseParams, error }, dispatch] = React.useReducer(reducer, initialState());

	// handle events from oauth client
	React.useEffect(() => {
		return oauth.registerDispatchFunc((action: oauth.Action) => {
			switch (action.type) {
				case oauth.ActionType.ERROR:
					dispatch({ type: ActionType.ERROR, error: action.error });
			}
		});
	}, []);

	// Handle any errors
	React.useEffect(() => {
		if (step !== Steps.ERROR) return;
		handleError(
			Object.assign(error, {
				retry: () => {
					dispatch({ type: ActionType.RESET });
				}
			})
		);
		console.error(error);
	}, [step, error, handleError]);

	// Open authorization window
	React.useEffect(() => {
		if (step !== Steps.UNAUTHORIZED || attempts >= maxAttempts) return () => {};

		let authWindow: ReturnType<typeof window.open>;

		const ac = new AbortController();
		const cancel = () => {
			ac.abort();
			if (authWindow) authWindow.close();
		};

		oauth.getToken((token: oauth.Token | null, error: Error | null) => {
			if (ac.signal.aborted === true) return;
			if (token) {
				Config.setAuthKey(token.access_token);
			} else {
				oauth
					.generateAuthorizationURL()
					.then((url: string) => {
						if (ac.signal.aborted === true) return;
						authWindow = window.open(url, 'OAuth', '');
						attempts++;
					})
					.catch((error) => {
						if (ac.signal.aborted === true) return;
						const e = new Error(`Error authenticating: ${error}`);
						dispatch({
							type: ActionType.ERROR,
							error: e
						});
					});
			}
		});
		return cancel;
	}, [step]);

	// Authorization callback event emitter
	if (step === Steps.AUTHORIZATION_CALLBACK) {
		const parent = window.opener;
		if (parent) {
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

	// Authorization callback event handler
	const receiveMessage = React.useCallback(
		(event: MessageEvent) => {
			if (step !== Steps.UNAUTHORIZED) return;
			if (event.origin !== getOrigin()) return;
			if (event.data.action !== 'callback') return;
			window.removeEventListener('message', receiveMessage);

			dispatch({ type: ActionType.AUTHORIZATION_CALLBACK, params: event.data.payload });
		},
		[step, dispatch]
	);

	React.useEffect(() => {
		if (step !== Steps.UNAUTHORIZED) return () => {};

		window.addEventListener('message', receiveMessage, false);
		const cancel = () => {
			window.removeEventListener('message', receiveMessage);
		};
		return cancel;
	}, [step, receiveMessage]);

	// Perform token exchange
	React.useEffect(() => {
		if (step !== Steps.TOKEN_EXCHANGE) return () => {};
		const ac = new AbortController();
		oauth
			.tokenExchange(
				// authorizationResponseParams should never be null on this step
				authorizationResponseParams as oauth.AuthorizationResponseParams,
				(token: oauth.Token | null, error: Error | null) => {
					if (ac.signal.aborted === true) return;
					if (error !== null) {
						throw error;
					}
					attempts = maxAttempts;
				},
				ac.signal
			)
			.catch((error) => {
				if (ac.signal.aborted === true) return;
				dispatch({ type: ActionType.ERROR, error });
			});
		const cancel = () => {
			ac.abort();
		};
		return cancel;
	}, [step, authorizationResponseParams]);

	// Listen for if the client becomes unauthorized
	React.useEffect(() => {
		const cancel = Config.authCallback((authenticated) => {
			if (authenticated) {
				// oauth-client calls Config.setAuthKey
				dispatch({ type: ActionType.AUTHORIZED });
			} else {
				// ignore the client until we've gone throw the flow
				if (step !== Steps.AUTHORIZED) return;

				// clear token data
				// the client probably got an unauthenticated response code
				oauth.reset().then(() => {
					// reset to the first step
					/* dispatch({ type: ActionType.RESET }); */
				});
			}
		});
		return cancel;
	}, [step]);

	if (step === Steps.AUTHORIZED || step === Steps.ERROR || attempts >= maxAttempts) {
		return null;
	}

	return (
		<RightOverlay>
			<h3>Authorizing...</h3>
			<p>Make sure popups are enabled</p>
		</RightOverlay>
	);
}
