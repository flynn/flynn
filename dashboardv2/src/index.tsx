import React from 'react';
import ReactDOM from 'react-dom';
import './index.css';
import Dashboard from './Dashboard';
import * as serviceWorker from './serviceWorker';
import ifDev from './ifDev';

// add insights into component re-renders in development
ifDev(() => {
	if (true) return; // disable why-did-you-render
	const whyDidYouRender = require('@welldone-software/why-did-you-render');
	whyDidYouRender(React, { include: /.*/, trackHooks: true });
});

ReactDOM.render(<Dashboard />, document.getElementById('root'));

// If you want your app to work offline and load faster, you can change
// unregister() to register() below. Note this comes with some pitfalls.
// Learn more about service workers: https://bit.ly/CRA-PWA
serviceWorker.unregister();
