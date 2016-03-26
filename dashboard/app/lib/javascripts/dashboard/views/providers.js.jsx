import Config from 'dashboard/config';
import Dispatcher from 'dashboard/dispatcher';
import { default as ProvidersStore, resourceAppsKey } from 'dashboard/stores/providers';
import ResourcesStore from 'dashboard/stores/resources';
import AppsStore from 'dashboard/stores/apps';
import Timestamp from 'dashboard/views/timestamp';
import RouteLink from 'dashboard/views/route-link';
import Provisioner from 'dashboard/views/provisioner';
import Resource from 'dashboard/views/resource';

var providersStoreID = 'default';
var resourcesStoreID = 'default';
var appsStoreID = null;

var providerAttrs = Config.PROVIDER_ATTRS;

var Providers = React.createClass({
	displayName: "Views.Providers",

	render: function () {
		var selectedResourceID = this.props.resourceID;
		var selectedResource = this.state.selectedResource;
		var providersByID = this.state.providersByID;
		var providerAppsByID = this.state.providerAppsByID;
		var resourceAppsIndex = this.state.resourceAppsIndex;
		var appsByID = this.state.appsByID;

		return (
			<section className='full-height' style={{
				display: 'flex',
				flexFlow: 'column',
				paddingBottom: '2rem',
				overflowY: 'auto'
			}}>
				<Provisioner resourceID={selectedResourceID} />

				<div className="panel" ref="scrollArea" style={{
					display: 'flex',
					overflowY: 'auto',
					marginTop: '1rem'
				}}>
					<div style={{
						minWidth: 260,
						width: 260
					}}>
						<ul className='items-list'>
							{providersByID !== null && appsByID !== null ? this.state.resources.map(function (resource) {
								var provider = providersByID[resource.provider];
								var pAttrs = providerAttrs[provider.name];
								var appNames = (resource.apps || []).map(function (appID) {
									return appsByID[appID].name;
								});
								return (
									<li key={resource.id} className={resource.id === selectedResourceID ? 'selected' : ''}>
										<RouteLink path={'/providers/'+ resource.provider +'/resources/'+ resource.id}>
											<div style={{
												display: 'table'
											}}>
												<img src={pAttrs.img} style={{ height: 40, marginRight: '0.25rem', display: 'table-cell', verticalAlign: 'middle' }} />
												<div style={{ display: 'table-cell', verticalAlign: 'middle' }}>
													<div>{pAttrs.title} ({appNames.length ? appNames.join(', ') : 'Standalone'})</div>
													<div><Timestamp timestamp={resource.created_at} /></div>
												</div>
											</div>
										</RouteLink>
									</li>
								);
							}, this) : null}
						</ul>
					</div>

					<div style={{
						paddingLeft: '2rem',
						paddingRight: '2rem',
						flexGrow: 2
					}}>
						{selectedResource && providersByID !== null ? (function (resource) {
							var provider = providersByID[resource.provider];
							var providerApp = providerAppsByID[provider.id];
							var resourceApp = resourceAppsIndex[resourceAppsKey(provider.id, resource.id)] || null;
							return (
								<Resource
									key={provider.id + resource.id}
									provider={provider}
									providerApp={providerApp}
									resource={resource}
									resourceApp={resourceApp} />
							);
						}.bind(this))(selectedResource) : (
							this.state.selectedResourceNotFound ? (
								<p className="placeholder">Resource not found</p>
							) : (
								<p className="placeholder">No resource selected</p>
							)
						)}
					</div>
				</div>
			</section>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props, {});
	},

	componentDidMount: function () {
		ProvidersStore.addChangeListener(providersStoreID, this.__handleStoreChange);
		ResourcesStore.addChangeListener(resourcesStoreID, this.__handleStoreChange);
		AppsStore.addChangeListener(appsStoreID, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		if (this.props.providerID !== nextProps.providerID) {
			ResourcesStore.removeChangeListener(resourcesStoreID, this.__handleStoreChange);
			ResourcesStore.addChangeListener(resourcesStoreID, this.__handleStoreChange);
		}
		if (this.props.resourceID !== nextProps.resourceID) {
			this.setState(this.__getState(nextProps, this.state));
			this.refs.scrollArea.getDOMNode().scrollTop = 0;
		}
	},

	componentWillUnmount: function () {
		ProvidersStore.removeChangeListener(providersStoreID, this.__handleStoreChange);
		ResourcesStore.removeChangeListener(resourcesStoreID, this.__handleStoreChange);
		AppsStore.removeChangeListener(appsStoreID, this.__handleStoreChange);
	},

	__handleProviderChange: function (providerID) {
		Dispatcher.dispatch({
			name: 'PROVIDER_SELECTED',
			providerID: providerID
		});
	},

	__handleProvisionBtnClick: function (providerID, e) {
		e.preventDefault();
		var newResourceState = this.state.newResourceStates[providerID] || {};
		if (newResourceState.isCreating) {
			return;
		}
		Dispatcher.dispatch({
			name: 'PROVISION_RESOURCE',
			providerID: providerID
		});
	},

	__toggleShowAdvanced: function () {
		this.setState(this.__getState(this.props, this.state, !this.state.showAdvanced));
	},

	__getState: function (props, prevState, showAdvanced) {
		var state = {
			showAdvanced: showAdvanced === undefined ? prevState.showAdvanced || false : showAdvanced
		};

		var providersState = ProvidersStore.getState(providersStoreID);
		var providersByID = {};
		providersState.providers.forEach(function (provider) {
			providersByID[provider.id] = provider;
		});
		state.providersByID = providersState.fetched ? providersByID : null;
		state.providerAppsByID = providersState.providerApps;
		state.resourceAppsIndex = providersState.resourceApps;

		var appsState = AppsStore.getState(appsStoreID);
		var appsByID = {};
		appsState.apps.forEach(function (app) {
			appsByID[app.id] = app;
		});
		state.appsByID = appsState.fetched ? appsByID : null;

		var resourcesState = ResourcesStore.getState(resourcesStoreID);
		state.resources = resourcesState.resources;
		state.selectedResource = (function (resourceID) {
			var resources = state.resources;
			for (var i = 0, len = resources.length; i < len; i++) {
				if (resources[i].id === resourceID) {
					return resources[i];
				}
			}
			return null;
		})(props.resourceID);
		state.selectedResourceNotFound = props.resourceID && !state.selectedResource && state.resourcesFetched;

		return state;
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props, this.state));
	}
});

export default Providers;
