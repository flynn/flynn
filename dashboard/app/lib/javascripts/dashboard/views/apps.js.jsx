/** @jsx React.DOM */
//= require ../stores/apps
//= require ./apps-list
//= require ./route-link
//= require ./app

(function () {

"use strict";

var AppsStore = Dashboard.Stores.Apps;

function getAppsStoreId () {
	return null;
}

function getState () {
	var state = {
		appsStoreId: getAppsStoreId()
	};

	var appsState = AppsStore.getState(state.appsStoreId);
	state.apps = appsState.apps;

	return state;
}

Dashboard.Views.Apps = React.createClass({
	displayName: "Views.Apps",

	render: function () {
		return (
			<section className="panel-row full-height">
				<section className="panel full-height apps-list-panel">
					<section className="clearfix">
						<Dashboard.Views.RouteLink
							className="btn-green float-right"
							path="/github">
								{this.props.githubAuthed ? (
									"Add Services"
								) : (
									<span className="connect-with-github">
										<i className="icn-github-mark" />
										Connect with Github
									</span>
								)}
						</Dashboard.Views.RouteLink>
					</section>

					<Dashboard.Views.AppsList
						selectedAppId={this.props.appProps.appId}
						getAppPath={this.props.getAppPath}
						apps={this.state.apps}
						defaultRouteDomain={this.props.defaultRouteDomain}
						showProtected={this.props.showProtected} />
				</section>

				<section className="panel app-panel">
					{this.props.appProps.appId ? (
						Dashboard.Views.App(Marbles.Utils.extend({}, this.props.appProps, { ref: "appComponent" }))
					) : (
						<p className="placeholder">No app selected</p>
					)}
				</section>
			</section>
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
