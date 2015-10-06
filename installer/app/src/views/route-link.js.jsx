import { pathWithParams } from 'marbles/history';
import { extend } from 'marbles/utils';
import Dispatcher from '../dispatcher';

var RouteLink = React.createClass({
	getInitialState: function () {
		return {
			href: null
		};
	},

	getDefaultProps: function () {
		return {
			className: null,
			path: "",
			pathPrefix: null,
			params: [{}]
		};
	},

	componentWillMount: function () {
		this.__setHrefFromPath(this.props.path, this.props.params);
	},

	componentWillReceiveProps: function (props) {
		this.__setHrefFromPath(props.path, props.params);
	},

	handleClick: function (e) {
		if (e.ctrlKey || e.metaKey || e.shiftKey) {
			return;
		}
		e.preventDefault();
		var options = {
			params: this.props.params
		};
		Dispatcher.dispatch({
			name: 'NAVIGATE',
			path: this.props.path,
			options: options
		});
	},

	__setHrefFromPath: function (path, params) {
		this.setState({
			href: pathWithParams(path, params)
		});
	},

	render: function () {
		var props = extend({}, this.props);
		props.href = this.state.href;
		props.onClick = this.handleClick;
		delete props.children;
		delete props.path;
		delete props.params;
		return React.createElement('a', props, this.props.children);
	}
});

export default RouteLink;
