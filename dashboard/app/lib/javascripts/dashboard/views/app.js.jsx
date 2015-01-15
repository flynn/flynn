//= require ../stores/app
//= require ./app-controls
//= require ./app-source-history
//= require ./service-unavailable

(function () {

"use strict";

var AppStore = Dashboard.Stores.App;

Dashboard.Views.App = React.createClass({
	displayName: "Views.App",

	render: function () {
		var app = this.state.app;

		return (
			<section>
				{ !app && this.state.serviceUnavailable ? (
					<Dashboard.Views.ServiceUnavailable status={503} />
				) : null }

				{ !app && this.state.notFound ? (
					<div>
						<h1>Not found</h1>
					</div>
				) : null }

				<section className="flex-row">
					{app ? (
						<section className="col app-controls-container">
							<Dashboard.Views.AppControls
								headerComponent={this.props.appControlsHeaderComponent}
								appId={this.props.appId}
								app={app}
								formation={this.state.formation}
								getAppPath={this.props.getAppPath} />
						</section>
					) : null}

					{app && app.meta && app.meta.type === "github" && Dashboard.githubClient ? (
						<section className="col">
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
		state.notFound = appState.notFound;
		state.app = appState.app;
		state.formation = appState.formation;

		return state;
	},

	__getClusterPathParams: function () {
		if (this.state.app && this.state.app.protected) {
			return [{ protected: "true" }];
		}
		return null;
	}
});

})();
