/** @jsx React.DOM */

(function () {

"use strict";

FlynnDashboard.Views.ExternalLink = React.createClass({
	displayName: "Views.ExternalLink",

	handleClick: function (e) {
		if (e.ctrlKey || e.metaKey || e.shiftKey) {
			return;
		}
		e.preventDefault();
		window.open(this.props.href);
	},

	render: function () {
		var props = Marbles.Utils.extend({}, this.props);
		props.onClick = this.handleClick;
		delete props.children;
		return React.DOM.a(props, this.props.children);
	}
});

})();
