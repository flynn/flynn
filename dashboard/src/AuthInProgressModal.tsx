import * as React from 'react';

import Config from './config';
import * as types from './worker/types';
import { addEventListener as addServerWorkerEventListener } from './serviceWorker';
import RightOverlay from './RightOverlay';
import Debounced from './Debounced';

export default function AuthInProgressModal() {
	const [authInProgress, setAuthInProgress] = React.useState(!Config.isAuthenticated());

	React.useEffect(() => {
		return addServerWorkerEventListener(types.MessageType.AUTH_REQUEST, (message: types.Message) => {
			setAuthInProgress(true);
		});
	}, []);

	React.useEffect(() => {
		return addServerWorkerEventListener(types.MessageType.AUTH_TOKEN, (message: types.Message) => {
			setAuthInProgress(false);
		});
	}, []);

	if (!authInProgress) return null;

	return (
		<Debounced timeoutMs={200}>
			<RightOverlay>
				<h3>Authorizing...</h3>
				<p>Make sure popups are enabled</p>
			</RightOverlay>
		</Debounced>
	);
}
