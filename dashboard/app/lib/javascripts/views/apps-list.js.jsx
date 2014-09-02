/** @jsx React.DOM */
//= require ../stores/apps
//= require ./external-link
//= require ./route-link

(function () {

"use strict";

var AppsStore = FlynnDashboard.Stores.Apps;

var ExternalLink = FlynnDashboard.Views.ExternalLink;
var RouteLink = FlynnDashboard.Views.RouteLink;

function getAppsStoreId () {
	return null;
}

function getState () {
	var state = {
		appsStoreId: getAppsStoreId()
	};

	var appsState = AppsStore.getState(state.appsStoreId);
	var groups = {};
	var services = [];
	appsState.apps.forEach(function (app) {
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

FlynnDashboard.Views.AppsList = React.createClass({
	displayName: "Views.AppsList",

	render: function () {
		var groups = this.state.groups;
		var services = this.state.services;

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
														path={Marbles.history.pathWithParams("/apps/:id", [{
															id: app.id
														}])}/>
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
											path={Marbles.history.pathWithParams("/apps/:id", [{
												id: app.id
											}])}/>
									)}
								</li>
							</ul>
						</li>
					);
				}.bind(this))}
			</ul>
		);
	},

	getInitialState: function () {
		return getState();
	},

	componentDidMount: function () {
		AppsStore.addChangeListener(this.state.appsStoreId, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		AppsStore.removeChangeListener(this.state.appsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(getState());
	}
});

})();
