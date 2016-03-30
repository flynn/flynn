import Router from 'marbles/router';
import WizardComponent from './views/wizard';
import ClusterDeleteComponent from './views/modal/cluster-delete';
import CredentialsComponent from './views/modal/credentials';
import Dispatcher from './dispatcher';
import QueryParams from 'marbles/query_params';

var MainRouter = Router.createClass({
	routes: [
		{ path: '', handler: 'landingPage' },
		{ path: '/clusters/:cluster_id', handler: 'landingPage' },
		{ path: '/clusters/:cluster_id/delete', handler: 'landingPage', modalHandler: 'clusterDeleteModal' },
		{ path: '/credentials', handler: 'landingPage', modalHandler: 'credentialsModal' },
		{ path: '/credentials/new', handler: 'landingPage', modalHandler: 'credentialsModal' },
		{ path: '/oauth/azure', handler: 'oauthAzureCallback' }
	],

	willInitialize: function () {
		Dispatcher.register(this.handleEvent.bind(this));
	},

	beforeHandler: function (event) {
		var clusterID = event.params[0].cluster_id || null;
		var state = event.context.dataStore.state;
		if (state.currentClusterID !== clusterID) {
			Dispatcher.dispatch({
				name: 'CURRENT_CLUSTER',
				clusterID: clusterID
			});
		}

		var cloudID = event.params[0].cloud || 'aws';
		if (this.history.getHandler(this.history.path).name === 'landingPage') {
			var currentCluster = state.currentCluster;
			if (currentCluster && currentCluster.attrs.ID === 'new' && state.currentCloudSlug !== cloudID) {
				Dispatcher.dispatch({
					name: 'SELECT_CLOUD',
					cloud: cloudID,
					clusterID: 'new'
				});
			}
		}

		if (event.handler.opts.hasOwnProperty('modalHandler')) {
			this[event.handler.opts.modalHandler].call(this, event.params, event.handler.opts, event.context);
		}
	},

	beforeHandlerUnload: function (event) {
		if (event.handler.opts.hasOwnProperty('modalHandler')) {
			event.context.unRenderModal();
		}
	},

	landingPage: function (params, opts, context) {
		var props = {
			dataStore: context.dataStore
		};
		context.render(WizardComponent, props);
	},

	clusterDeleteModal: function (params, opts, context) {
		var props = {
			clusterID: params[0].cluster_id
		};
		context.renderModal(ClusterDeleteComponent, props);
	},

	credentialsModal: function (params, opts, context) {
		var props = {
			dataStore: context.dataStore,
			cloud: params[0].cloud || 'aws',
			clusterID: params[0].cluster_id || 'new'
		};
		context.renderModal(CredentialsComponent, props);
	},

	oauthAzureCallback: function (params) {
		var clientID = window.localStorage.getItem("azureClientID");
		var name = window.localStorage.getItem("azureCredName");
		var endpoint = window.localStorage.getItem("azureEndpoint");
		var clusterID = window.localStorage.getItem("azureClusterID");
		window.localStorage.removeItem("azureClientID");
		window.localStorage.removeItem("azureCredName");
		window.localStorage.removeItem("azureEndpoint");
		window.localStorage.removeItem("azureClusterID");
		Dispatcher.dispatch({
			name: 'CREATE_CREDENTIAL',
			data: {
				name: name,
				id: clientID,
				secret: params[0].code,
				endpoint: endpoint,
				type: 'azure'
			}
		});
		Dispatcher.dispatch({
			name: 'SELECT_CREDENTIAL',
			credentialID: clientID,
			clusterID: 'new'
		});
		if (clusterID === 'new') {
			this.history.navigate('', {
				params: [{
					cloud: 'azure'
				}]
			});
		} else {
			Dispatcher.dispatch({
				name: 'PROMPT_SELECT_CREDENTIAL',
				credentialID: clientID,
				clusterID: clusterID
			});
			this.history.navigate('/clusters/'+ encodeURIComponent(clusterID));
		}
	},

	handleEvent: function (event) {
		var clusterID;
		switch (event.name) {
		case 'CLUSTER_DELETE':
			this.history.navigate('/clusters/'+ event.clusterID +'/delete');
			break;

		case 'CANCEL_CLUSTER_DELETE':
			if (this.history.getHandler().opts.modalHandler === 'clusterDeleteModal' && this.history.pathParams[0].cluster_id === event.clusterID) {
				this.history.navigate('/clusters/'+ event.clusterID);
			}
			break;

		case 'CONFIRM_CLUSTER_DELETE':
			if (this.history.getHandler().opts.modalHandler === 'clusterDeleteModal' && this.history.pathParams[0].cluster_id === event.clusterID) {
				this.history.navigate('/clusters/'+ event.clusterID);
			}
			break;

		case 'CLUSTER_STATE':
			if (event.state === 'deleted') {
				if (this.history.pathParams[0].cluster_id === event.clusterID) {
					this.history.navigate('/');
				}
			}
			break;

		case 'INSTALL_ABORT':
			this.history.navigate('/');
			break;

		case 'LAUNCH_CLUSTER_SUCCESS':
			clusterID = event.clusterID;
			this.history.navigate('/clusters/'+ clusterID);
			break;

		case 'NAVIGATE':
			this.history.navigate(event.path, event.options || {});
			break;

		case 'SELECT_CLOUD':
			if (this.history.getHandler(this.history.path).route.source !== '^$') {
				return;
			}
			if (this.history.pathParams[0].cloud === event.cloud) {
				return;
			}
			this.history.navigate('', {
				params: [{
					cloud: event.cloud
				}]
			});
			break;

		case 'AZURE_OAUTH_AUTHORIZE':
			window.localStorage.setItem("azureEndpoint", event.endpoint);
			window.localStorage.setItem("azureClientID", event.clientID);
			window.localStorage.setItem("azureCredName", event.credName);
			window.localStorage.setItem("azureClusterID", event.clusterID);
			var authorizeURL = event.endpoint.replace(/\/token.*$/, '') + '/authorize';
			authorizeURL += QueryParams.serializeParams([{
				client_id: event.clientID,
				response_type: 'code',
				resource: 'https://management.core.windows.net'
			}]);
			window.location.href = authorizeURL;
			break;
		}
	}
});
export default MainRouter;
