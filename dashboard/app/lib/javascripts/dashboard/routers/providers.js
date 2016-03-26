import Router from 'marbles/router';
import State from 'marbles/state';
import { default as ProvidersStore, resourceAppsKey } from 'dashboard/stores/providers';
import ProvidersComponent from 'dashboard/views/providers';
import CreateExternalProviderRouteComponent from 'dashboard/views/provider-route-create';
import ResourceDeleteComponent from 'dashboard/views/resource-delete';
import Dispatcher from 'dashboard/dispatcher';

var providersStoreID = 'default';

var ProvidersRouter = Router.createClass({
	displayName: "routers.providers",

	routes: [
		{ path: "providers", handler: "providers" },
		{ path: "providers/:providerID/create-external-route", handler: "createExternalProviderRoute", secondary: true },
		{ path: "providers/:providerID/resources/:resourceID/create-external-route", handler: "createExternalProviderRoute", secondary: true },
		{ path: "providers/:providerID/resources/:resourceID", handler: "resource" },
		{ path: "providers/:providerID/resources/:resourceID/delete", handler: "resourceDelete", secondary: true },
		{ path: "providers/:providerID/resources/:resourceID/apps/:appID/delete", handler: "resourceDeleteApp", secondary: true }
	],

	mixins: [State],

	willInitialize: function () {
		this.dispatcherIndex = Dispatcher.register(this.handleEvent.bind(this));
		this.state = {};
		this.__changeListeners = []; // always empty

		this.__providersStoreChangeListener = function(){};
	},

	beforeHandler: function () {
		ProvidersStore.addChangeListener(providersStoreID, this.__providersStoreChangeListener);
	},

	beforeHandlerUnload: function () {
		ProvidersStore.removeChangeListener(providersStoreID, this.__providersStoreChangeListener);
	},

	providers: function () {
		var props = {
			providerID: null,
			resourceID: null
		};
		var view = this.context.primaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.Providers") {
			view.setProps(props);
		} else {
			this.context.primaryView = React.render(React.createElement(
				ProvidersComponent, props), this.context.el);
		}
	},

	createExternalProviderRoute: function (params) {
		params = params[0];

		var shouldProvisionResource = params.provision === 'true';

		this.setState({
			creatingExternalRoute: true,
			providerID: params.providerID,
			resourceID: params.resourceID,
			provisionResource: shouldProvisionResource
		});

		this.context.secondaryView = React.render(React.createElement(
			CreateExternalProviderRouteComponent,
			{
				key: params.providerID,
				providerID: params.providerID,
				resourceID: params.resourceID,
				provisionResource: shouldProvisionResource,
				onHide: function () {
					if (params.resourceID) {
						this.history.navigate('/providers/'+ params.providerID +'/resources/'+ params.resourceID, { replace: true });
					} else {
						this.history.navigate('/providers', { replace: true });
					}
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		if (params.resourceID) {
			// render resource view in background
			this.resource.apply(this, arguments);
		} else {
			// render providers view in background
			this.providers.apply(this, arguments);
		}
	},

	resource: function (params) {
		params = params[0];
		var props = {
			providerID: params.providerID || null,
			resourceID: params.resourceID || null
		};
		var view = this.context.primaryView;
		if (view && view.isMounted() && view.constructor.displayName === "Views.Providers") {
			view.setProps(props);
		} else {
			this.context.primaryView = React.render(React.createElement(
				ProvidersComponent, props), this.context.el);
		}
	},

	resourceDelete: function (params) {
		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			ResourceDeleteComponent,
			{
				key: params.resourceID,
				providerID: params.providerID || null,
				resourceID: params.resourceID || null,
				appID: null,
				onHide: function () {
					this.history.navigate('/providers/'+ params.providerID +'/resources/'+ params.resourceID);
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render resource view in background
		this.resource.apply(this, arguments);
	},

	resourceDeleteApp: function (params) {
		params = params[0];

		this.context.secondaryView = React.render(React.createElement(
			ResourceDeleteComponent,
			{
				key: params.resourceID,
				providerID: params.providerID,
				resourceID: params.resourceID,
				appID: params.appID,
				onHide: function () {
					this.history.navigate('/providers/'+ params.providerID +'/resources/'+ params.resourceID);
				}.bind(this)
			}),
			this.context.secondaryEl
		);

		// render resource view in background
		this.resource.apply(this, arguments);
	},

	handleEvent: function (event) {
		switch (event.name) {
		case 'handler:before':
			// reset state between routes
			if (event.path.match(/^providers/)) {
				this.state = {
					providerID: event.params[0].providerID,
					resourceID: event.params[0].resourceID,
					loaded: true
				};
			} else {
				this.state = {
					loaded: false
				};
			}
			React.unmountComponentAtNode(this.context.secondaryEl);
			break;
		case 'PROVISION_RESOURCE':
			this.setState({
				newResourceProviderID: event.providerID,
				createRoute: !!event.createRoute,
				routeServiceName: event.createRoute ? event.createRoute.serviceName : null,
				routeServiceNameFromResource: event.createRoute ? event.createRoute.serviceNameFromResource || null : null
			});
			break;
		case 'RESOURCE':
			if (this.state.newResourceProviderID === event.data.provider) {
				if (this.state.createRoute) {
					this.setState({
						resource: event.data,
						routeServiceName: this.state.routeServiceNameFromResource ? this.state.routeServiceNameFromResource(event.data) : this.state.routeServiceName
					});
				} else {
					this.history.navigate('/providers/'+ event.data.provider +'/resources/'+ event.object_id);
				}
			}
			break;
		case 'ROUTE':
			if (this.state.createRoute && this.state.resource && event.data.service === this.state.routeServiceName) {
				this.history.navigate('/providers/'+ this.state.newResourceProviderID +'/resources/'+ this.state.resource.id);
			} else if (this.state.creatingExternalRoute && !this.state.provisionResource) {
				var resourceApp = ProvidersStore.getState(providersStoreID).resourceApps[resourceAppsKey(this.state.providerID, this.state.resourceID)] || null;
				if (resourceApp && resourceApp.id === event.app) {
					this.history.navigate('/providers/'+ this.state.providerID +'/resources/'+ this.state.resourceID);
				}
			}
			break;
		case 'RESOURCE_DELETED':
		case 'RESOURCE_APP_DELETED':
			if (this.state.loaded && this.state.resourceID === event.object_id) {
				this.history.navigate('/providers');
			}
			break;
		}
	}

});

export default ProvidersRouter;
