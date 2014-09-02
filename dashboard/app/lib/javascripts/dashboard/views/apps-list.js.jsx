/** @jsx React.DOM */
//= require ./route-link

(function () {

"use strict";

var RouteLink = Dashboard.Views.RouteLink;

function getState(props) {
	var state = {};

	var groups = {};
	var services = [];
	props.apps.forEach(function (app) {
		var repoFullName;
		if (app.meta && app.meta.type === "github") {
			repoFullName = app.meta.user_login +"/"+ app.meta.repo_name;
			groups[repoFullName] = groups[repoFullName] || [];
			groups[repoFullName].push(app);
		} else {
			services.push(app);
		}
	});
	state.groups = groups;
	state.services = services;

	return state;
}

Dashboard.Views.AppsList = React.createClass({
	displayName: "Views.AppsList",

	render: function () {
		var groups = this.state.groups;
		var services = this.state.services;

		var getAppPath = this.props.getAppPath;

		return (
			<ul className="apps-list">
				{Object.keys(groups).sort().map(function (key) {
					var apps = groups[key];
					return (
						<li key={key} className="repo">
							<span className="name">{key}</span>

							<ul>
								{apps.map(function (app) {
									return (
										<li key={app.id}>
											<span className="name">
												{app.meta.ref}
											</span>
											<ul className="actions">
												<li>
													<RouteLink
														className="icn-edit"
														path={getAppPath(app.id)}/>
												</li>
											</ul>
										</li>
									);
								}.bind(this))}
							</ul>
						</li>
					);
				}.bind(this))}

				{services.map(function (app) {
					return (
						<li key={app.id} className={"service"+ (app.protected ? " protected" : "")}>
							<span className="name">{app.name}</span>
							<ul className="actions">
								<li>
									{app.protected ? (
										<span className="icn-edit" />
									) : (
										<RouteLink
											className="icn-edit"
											path={getAppPath(app.id)}/>
									)}
								</li>
							</ul>
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
