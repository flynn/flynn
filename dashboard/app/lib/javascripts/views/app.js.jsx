/** @jsx React.DOM */
//= require ../stores/app
//= require ./app-controls
//= require ./app-source-history
//= require ./service-unavailable
//= require ./route-link

(function () {

"use strict";

var AppStore = FlynnDashboard.Stores.App;

var RouteLink = FlynnDashboard.Views.RouteLink;

function getAppStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props) {
	var state = {
		appStoreId: getAppStoreId(props)
	};

	var appState = AppStore.getState(state.appStoreId);
	state.serviceUnavailable = appState.serviceUnavailable;
	state.app = appState.app;
	state.formation = appState.formation;

	return state;
}

FlynnDashboard.Views.App = React.createClass({
	displayName: "Views.App",

	render: function () {
		var app = this.state.app;

		return (
			<section>
				<RouteLink path="/" className="back-link">
					Go back to cluster
				</RouteLink>

				{ !app && this.state.serviceUnavailable ? (
					<FlynnDashboard.Views.ServiceUnavailable status={503} />
				) : null }

				{app ? (
					<section className="panel">
						<FlynnDashboard.Views.AppControls
							appId={this.props.appId}
							app={app}
							formation={this.state.formation}
							getAppPath={this.props.getAppPath} />
					</section>
				) : null}

				{app && app.meta && app.meta.type === "github" ? (
					<section className="panel">
						<FlynnDashboard.Views.AppSourceHistory
							appId={this.props.appId}
							app={app}
							selectedBranchName={this.props.selectedBranchName}
							selectedSha={this.props.selectedSha}
							selectedTab={this.props.selectedTab}
							getAppPath={this.props.getAppPath} />
					</section>
				) : null}
			</section>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppStoreId = this.state.appStoreId;
		var nextAppStoreId = getAppStoreId(nextProps);
		if ( !Marbles.Utils.assertEqual(prevAppStoreId, nextAppStoreId) ) {
			AppStore.removeChangeListener(prevAppStoreId, this.__handleStoreChange);
			AppStore.addChangeListener(nextAppStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppStore.removeChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	}
});

})();
