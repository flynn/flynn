//= require ../stores/app-resources

(function () {

"use strict";

var AppResourcesStore = Dashboard.Stores.AppResources;

function getAppResourcesStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props) {
	var state = {
		appResourcesStoreId: getAppResourcesStoreId(props)
	};

	var appResourcesState = AppResourcesStore.getState(state.appResourcesStoreId);
	state.resources = appResourcesState.resources;
	state.resourcesFetched = appResourcesState.fetched;

	return state;
}

Dashboard.Views.AppResources = React.createClass({
	displayName: "Views.AppResources",

	render: function () {
		return (
			<section className="app-resources">
				<header>
					<h2>Databases</h2>
				</header>

				{(this.state.resources.length === 0 && this.state.resourcesFetched) ? (
					<span>(none)</span>
				) : (
					<ul>
						{this.state.resources.map(function (resource) {
							return (
								<li key={resource.id}>
									{resource.provider}
								</li>
							);
						}, this)}
					</ul>
				)}
			</section>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppResourcesStore.addChangeListener(this.state.appResourcesStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppResourcesStoreId = this.state.appResourcesStoreId;
		var nextAppResourcesStoreId = getAppResourcesStoreId(nextProps);
		if ( !Marbles.Utils.assertEqual(prevAppResourcesStoreId, nextAppResourcesStoreId) ) {
			AppResourcesStore.removeChangeListener(prevAppResourcesStoreId, this.__handleStoreChange);
			AppResourcesStore.addChangeListener(nextAppResourcesStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppResourcesStore.removeChangeListener(this.state.appResourcesStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props));
	}
});

})();
