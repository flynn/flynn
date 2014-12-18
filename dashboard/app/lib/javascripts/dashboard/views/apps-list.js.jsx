/** @jsx React.DOM */
//= require ./route-link
//= require ./external-link

(function () {

"use strict";

var RouteLink = Dashboard.Views.RouteLink;

function getState(props) {
	var state = {};

	var showProtected = props.showProtected;
	state.apps = props.apps.filter(function (app) {
		return !app.protected || showProtected;
	});

	return state;
}

Dashboard.Views.AppsList = React.createClass({
	displayName: "Views.AppsList",

	render: function () {
		var apps = this.state.apps;

		var getAppPath = this.props.getAppPath;
		var selectedAppId = this.props.selectedAppId;

		return (
			<ul className="apps-list">
				{apps.map(function (app) {
					return (
						<li key={app.id} className={app.id === selectedAppId ? "selected" : ""}>
							<RouteLink path={getAppPath(app.id)}>
								{app.name}
							</RouteLink>
						</li>
					);
				}.bind(this))}
			</ul>
		);
	},

	getDefaultProps: function () {
		return {
			apps: [],
			getAppPath: function (appId) {
				return Marbles.history.pathWithParams("/apps/:id", [{ id: appId }]);
			}
		};
	},

	componentWillMount: function () {
		this.setState(getState(this.props));
	},

	componentWillReceiveProps: function (props) {
		this.setState(getState(props));
	}
});

})();
