//= require ../stores/apps
//= require ./apps-list-header
//= require ./apps-list
//= require ./route-link
//= require ./app

(function () {

"use strict";

var AppsStore = Dashboard.Stores.Apps;

Dashboard.Views.Apps = React.createClass({
	displayName: "Views.Apps",

	render: function () {
		return (
			<section className="panel-row full-height">
				<section className="panel full-height apps-list-panel">
					{React.createElement(Dashboard.Views.AppsListHeader, this.props.appsListHeaderProps || {})}

					{React.createElement(Dashboard.Views.AppsList, Marbles.Utils.extend({}, this.props.appsListProps, {
						apps: this.state.apps
					}))}
				</section>

				<section className="panel app-panel">
					{this.props.appProps.appId ? (
						React.createElement(Dashboard.Views.App, Marbles.Utils.extend({}, this.props.appProps, { ref: "appComponent" }))
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

})();
