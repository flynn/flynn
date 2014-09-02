/** @jsx React.DOM */

(function () {

"use strict";

FlynnDashboard.Views.RouteLink = React.createClass({
	displayName: "Views.RouteLink",

	getInitialState: function () {
		return {
			href: null
		};
	},

	getDefaultProps: function () {
		return {
			className: null,
			path: ""
		};
	},

	componentWillMount: function () {
		this.__setHrefFromPath(this.props.path);
	},

	componentWillReceiveProps: function (props) {
		this.__setHrefFromPath(props.path);
	},

	handleClick: function (e) {
		if (e.ctrlKey || e.metaKey || e.shiftKey) {
			return;
		}
		e.preventDefault();
		Marbles.history.navigate(this.props.path);
		return false;
	},

	__setHrefFromPath: function (path) {
		var href;
		if (FlynnDashboard.config.PATH_PREFIX === null) {
			href = path;
		} else {
			href = FlynnDashboard.config.PATH_PREFIX + path;
		}
		this.setState({ href: href });
	},

	render: function () {
		var props = Marbles.Utils.extend({}, this.props);
		props.href = this.state.href;
		props.onClick = this.handleClick;
		delete props.children;
		delete props.path;
		return React.DOM.a(props, this.props.children);
	},
});

})();
