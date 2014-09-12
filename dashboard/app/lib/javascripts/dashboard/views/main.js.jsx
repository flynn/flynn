/** @jsx React.DOM */
//= require ../stores/apps
//= require ./apps-list
//= require ./route-link
//= require ../actions/main

(function () {

"use strict";

var AppsStore = Dashboard.Stores.Apps;

var MainActions = Dashboard.Actions.Main;

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

Dashboard.Views.Main = React.createClass({
	displayName: "Views.Main",

	render: function () {
		return (
			<section className="panel">
				<section>
					<Dashboard.Views.AppsList apps={this.state.apps} />
				</section>

				<section className="clearfix">
					<button className="logout-btn" onClick={MainActions.handleLoginBtnClick}>Log out</button>
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
