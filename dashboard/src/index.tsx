import React from 'react';
import ReactDOM from 'react-dom';
import './index.css';
import Dashboard from './Dashboard';
import * as serviceWorker from './serviceWorker';
import * as workerTypes from './worker/types';
import ifDev from './ifDev';

function getOrigin(): string {
	return `${window.location.protocol}//${window.location.host}`;
}

// add insights into component re-renders in development
ifDev(() => {
	if (true) return; // disable why-did-you-render
	const whyDidYouRender = require('@welldone-software/why-did-you-render');
	whyDidYouRender(React, { include: /.*/, trackHooks: true });
});

if (window.location.pathname === '/oauth/callback') {
	// OAuth callback window, so don't render React
	if (window.opener) {
		window.opener.postMessage({
			type: workerTypes.MessageType.AUTH_CALLBACK,
			payload: window.location.hash.substr(1)
		});
		window.close();
	} else {
		// if there's no window.opener then it's probably a mistake so redirect to
		// the main app
		window.location.href = getOrigin();
	}
} else {
	ReactDOM.render(<Dashboard />, document.getElementById('root'));

	serviceWorker.register().then(() => {
		// handle OAuth callback window sending message
		// and forward it along to the service worker
		const receiveMessage = (event: MessageEvent) => {
			if (event.origin !== getOrigin()) return;
			const message = event.data as workerTypes.AuthCallbackMessage;
			if (message.type !== workerTypes.MessageType.AUTH_CALLBACK) return;
			window.removeEventListener('message', receiveMessage);
			serviceWorker.postMessage(message);
		};
		window.addEventListener('message', receiveMessage);
	});
}
