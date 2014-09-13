/** @jsx React.DOM */
//= require ../stores/app
//= require ./app-controls
//= require ./app-source-history
//= require ./service-unavailable
//= require ./route-link

(function () {

"use strict";

var AppStore = Dashboard.Stores.App;

var RouteLink = Dashboard.Views.RouteLink;

Dashboard.Views.App = React.createClass({
	displayName: "Views.App",

	render: function () {
		var app = this.state.app;

		return (
			<section>
				<RouteLink path={this.props.getClusterPath()} className="back-link">
					Go back to cluster
				</RouteLink>

				{ !app && this.state.serviceUnavailable ? (
					<Dashboard.Views.ServiceUnavailable status={503} />
				) : null }

				{app ? (
					<section className="panel">
						<Dashboard.Views.AppControls
							appId={this.props.appId}
							app={app}
							formation={this.state.formation}
							getAppPath={this.props.getAppPath} />
					</section>
				) : null}

				{app && app.meta && app.meta.type === "github" ? (
					<section className="panel">
						<Dashboard.Views.AppSourceHistory
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
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		AppStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppStoreId = this.state.appStoreId;
		var nextAppStoreId = this.__getAppStoreId(nextProps);
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
		this.setState(this.__getState(props || this.props));
	},

	__getAppStoreId: function (props) {
		return {
			appId: props.appId
		};
	},

	__getState: function (props) {
		var state = {
			appStoreId: this.__getAppStoreId(props)
		};

		var appState = AppStore.getState(state.appStoreId);
		state.serviceUnavailable = appState.serviceUnavailable;
		state.app = appState.app;
		state.formation = appState.formation;

		return state;
	}
});

})();
