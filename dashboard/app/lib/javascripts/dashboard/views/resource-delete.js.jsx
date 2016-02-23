import Config from 'dashboard/config';
import Dispatcher from 'dashboard/dispatcher';
import Modal from 'Modal';
import ProvidersStore from 'dashboard/stores/providers';
import ResourcesStore from 'dashboard/stores/resources';
import AppsStore from 'dashboard/stores/apps';
import RouteLink from 'dashboard/views/route-link';

var providersStoreID = 'default';
var resourcesStoreID = 'default';
var appsStoreID = null;

var ResourceDelete = React.createClass({
	displayName: "Views.ResourceDelete",

	render: function () {
		var pAttrs = this.state.providerAttrs;
		var app = this.state.app;
		var otherResourceApps = this.state.otherResourceApps;
		var isDeleting = this.state.isDeleting;

		var appsList = (
			<ul key="apps-list">
				{otherResourceApps.map(function (otherApp) {
					return (
						<li key={otherApp.id}>
							<RouteLink path={'/apps/'+ otherApp.id}>{otherApp.name}</RouteLink>
						</li>
					);
				})}
			</ul>
		);

		var singleAppWarning = function (app) {
			return (
				<p className="alert-warning">This will delete the instance of {pAttrs.title} used by <RouteLink path={'/apps/'+ app.id}>{app.name}</RouteLink>. All will be destroyed.</p>
			);
		};

		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-delete-resource">
					<header>
						<h1>Remove {pAttrs.title}</h1>
					</header>

					{this.props.appID ? (otherResourceApps.length ? (
						<div className="alert-info">
							This will unlink <RouteLink path={'/apps/'+ app.id}>{app.name}</RouteLink> from {pAttrs.title}. All data will remain intact for use by the following apps:
							{appsList}
							You may delete the resource completely <RouteLink path={'/providers/'+ this.props.providerID +'/resources/'+ this.props.resourceID +'/delete'}>here</RouteLink>.
						</div>
					) : singleAppWarning(app)) : (otherResourceApps.length === 1 ? singleAppWarning(otherResourceApps[0]) : (
						<div className="alert-warning">
							This instance of {pAttrs.title} will be removed and all it's data.
							{otherResourceApps.length ? [
								' The following apps will be effected:',
								appsList
							] : null}
						</div>
					))}

					<button disabled={isDeleting} className="delete-btn" onClick={this.__handleDeleteBtnClick}>
						{isDeleting ? 'Please wait...' : 'I understand, remove '+ pAttrs.title}
					</button>
				</section>
			</Modal>
		);
	},

	__handleDeleteBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'DELETE_RESOURCE',
			appID: this.props.appID,
			providerID: this.props.providerID,
			resourceID: this.props.resourceID
		});
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	__getState: function (props) {
		var state = {};

		var providersState = ProvidersStore.getState(providersStoreID);
		var provider = providersState.providers.find(function (provider) {
			return provider.id === props.providerID;
		});
		state.providerAttrs = provider ? Config.PROVIDER_ATTRS[provider.name] : {title: ''};

		state.isDeleting = (providersState.deletingResourceStates[props.resourceID] || {}).isDeleting || false;

		var appsState = AppsStore.getState(appsStoreID);
		var appsByID = {};
		appsState.apps.forEach(function (app) {
			appsByID[app.id] = app;
		});
		state.app = appsByID[props.appID] || {name: ''};

		var resourcesState = ResourcesStore.getState(resourcesStoreID);
		state.otherResourceApps = ((resourcesState.resources.find(function (resource) {
			return resource.id === props.resourceID;
		}) || {}).apps || []).filter(function (appID) {
			return appID !== props.appID;
		}).map(function (appID) {
			return appsByID[appID] || {name: ''};
		});

		return state;
	},

	componentDidMount: function () {
		ProvidersStore.addChangeListener(providersStoreID, this.__handleStoreChange);
		ResourcesStore.addChangeListener(resourcesStoreID, this.__handleStoreChange);
		AppsStore.addChangeListener(appsStoreID, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		ProvidersStore.removeChangeListener(providersStoreID, this.__handleStoreChange);
		ResourcesStore.removeChangeListener(resourcesStoreID, this.__handleStoreChange);
		AppsStore.removeChangeListener(appsStoreID, this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props));
	}
});

export default ResourceDelete;
