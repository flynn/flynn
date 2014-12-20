/** @jsx React.DOM */

(function () {

"use strict";

Dashboard.Views.ExternalLink = React.createClass({
	displayName: "Views.ExternalLink",

	handleClick: function (e) {
		if (this.props.onClick) {
			this.props.onClick();
		}
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
