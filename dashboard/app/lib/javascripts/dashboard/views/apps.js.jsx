import { extend } from 'marbles/utils';
import AppsStore from '../stores/apps';
import AppsListHeader from './apps-list-header';
import AppsList from './apps-list';
import App from './app';
import RouteLink from './route-link';

var Apps = React.createClass({
	displayName: "Views.Apps",

	render: function () {
		return (
			<section className="panel-row full-height">
				<section className="panel full-height apps-list-panel">
					{React.createElement(AppsListHeader, this.props.appsListHeaderProps || {})}

					{React.createElement(AppsList, extend({}, this.props.appsListProps, {
						apps: this.state.apps
					}))}

					<section className="system-applications-link">
						<RouteLink path={"/apps"} params={{system: !this.props.showSystemApps }}>
							{this.props.showSystemApps ? 'Hide' : 'Show'} System Applications
						</RouteLink>
					</section>
				</section>

				<section className="panel app-panel full-height">
					{this.props.appProps.appId ? (
						React.createElement(App, extend({}, this.props.appProps, { ref: "appComponent" }))
					) : (
						<p className="placeholder">No app selected</p>
					)}
				</section>
			</section>
		);
	},

	__getAppsStoreId: function () {
		return null;
	},

	__getState: function (props) {
		var state = {
			appsStoreId: this.__getAppsStoreId(props)
		};

		var appsState = AppsStore.getState(state.appsStoreId);
		state.apps = appsState.apps;

		return state;
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		AppsStore.addChangeListener(this.state.appsStoreId, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		AppsStore.removeChangeListener(this.state.appsStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props));
	}
});

export default Apps;
