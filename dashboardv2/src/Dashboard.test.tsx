import * as React from 'react';
import * as ReactDOM from 'react-dom';
import Dashboard from './Dashboard';

it('renders without crashing', () => {
	const div = document.createElement('div');
	ReactDOM.render(<Dashboard />, div);
	ReactDOM.unmountComponentAtNode(div);
});
