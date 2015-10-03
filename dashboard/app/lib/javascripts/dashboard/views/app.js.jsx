import { assertEqual } from 'marbles/utils';
import Config from '../config';
import AppStore from '../stores/app';
import AppSourceHistory from './app-source-history';
import AppControls from './app-controls';
import AppHistory from './app-history';
import ServiceUnavailable from './service-unavailable';

var isSystemApp = AppStore.isSystemApp;

var App = React.createClass({
	displayName: "Views.App",

	render: function () {
		var app = this.state.app;
		var release = this.state.release;
		var formation = this.state.formation;

		if ( !app && this.state.serviceUnavailable ) {
			return (
				<section>
					<section className="flex-row">
						<ServiceUnavailable status={503} />;
					</section>
				</section>
			);
		}

		if ( !app && this.state.notFound ) {
			return (
				<section>
					<section className="flex-row">
						<div>
							<h1>Not found</h1>
						</div>
					</section>
				</section>
			);
		}

		return (
			<section>
				<section className="flex-row">
					{app ? (
						<section className="col app-controls-container">
							<AppControls
								headerComponent={this.props.appControlsHeaderComponent}
								appId={this.props.appId}
								app={app}
								formation={formation}
								getAppPath={this.props.getAppPath} />
						</section>
					) : null}

					<section className="col">
						<section>
							<AppHistory
								key={this.props.appId}
								appID={this.props.appId}
								release={release}
								formation={formation} />
						</section>

						{release && release.meta && release.meta.github === "true" && Config.githubClient ? (
							<section>
								<AppSourceHistory
									appId={this.props.appId}
									app={app}
									release={release}
									selectedBranchName={this.props.selectedBranchName}
									selectedSha={this.props.selectedSha}
									selectedTab={this.props.selectedTab}
									getAppPath={this.props.getAppPath} />
							</section>
						) : null}
					</section>
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
		if ( !assertEqual(prevAppStoreId, nextAppStoreId) ) {
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
		state.release = appState.release;

		return state;
	},

	__getClusterPathParams: function () {
		if (this.state.app && isSystemApp(this.state.app)) {
			return [{ system: "true" }];
		}
		return null;
	}
});

export default App;
