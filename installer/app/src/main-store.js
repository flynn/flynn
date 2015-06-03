import { createClass, extend } from 'marbles/utils';
import State from 'marbles/state';
import Client from './client';
import Cluster from './cluster';
import Dispatcher from './dispatcher';

var newCluster = new Cluster({id: 'new'});

export default createClass({
	mixins: [State],

	registerWithDispatcher: function (dispatcher) {
		this.dispatcherIndex = dispatcher.register(this.handleEvent.bind(this));
	},

	willInitialize: function () {
		this.registerWithDispatcher(Dispatcher);

		this.__handleClusterChanged = this.__handleClusterChanged.bind(this);
		this.state = this.getInitialState();
		newCluster.addChangeListener(this.__handleClusterChanged);
		this.__changeListeners = [];
	},

	getInitialState: function () {
		return {
			clusters: [],
			credentials: [],
			currentClusterID: null,
			currentCloudSlug: 'aws',
			currentCluster: newCluster,
			errors: {
				credentials: []
			}
		};
	},

	handleEvent: function (event) {
		var cluster;
		switch (event.name) {
			case 'LAUNCH_AWS':
				this.launchAWS(event);
			break;

			case 'LAUNCH_DIGITAL_OCEAN':
				this.launchDigitalOcean(event);
			break;

			case 'LAUNCH_AZURE':
				this.launchAzure(event);
			break;

			case 'NEW_CLUSTER':
				this.__addCluster(event.cluster);
				if (this.__pendingCurrentClusterID === event.cluster.ID) {
					this.__pendingCurrentClusterID = null;
					this.setState({
						currentClusterID: event.cluster.ID,
						currentCluster: event.cluster,
						currentCloudSlug: event.cluster.type
					});
				}
			break;

			case 'NEW_CREDENTIAL':
				this.__addCredential(event.credential);
			break;

			case 'CREDENTIAL_DELETED':
				this.__removeCredential(event.id);
			break;

			case 'CURRENT_CLUSTER':
				cluster = event.clusterID === null ? newCluster : this.__findCluster(event.clusterID);
				if ( !cluster ) {
					this.__pendingCurrentClusterID = event.clusterID;
					break;
				}
				this.setState({
					currentClusterID: event.clusterID,
					currentCluster: cluster,
					currentCloudSlug: cluster.type
				});
			break;

			case 'CREATE_CREDENTIAL':
				this.__clearErrors('credentials');
				if (this.__findCredentialIndex(event.data.id) !== -1) {
					return;
				}
				this.__addCredential(event.data);
				this.__createCredPromise = Client.createCredential(event.data).catch(function (args) {
					var xhr = args[1];
					if (xhr.status === 409) {
						return; // Already exists
					}
					this.__removeCredential(event.data.id);
				}.bind(this));
			break;

			case 'DELETE_CREDENTIAL':
				this.__clearErrors('credentials');
				this.__removeCredential(event.creds.id);
				Client.deleteCredential(event.creds.type, event.creds.id).catch(function (args) {
					var xhr = args[1];
					if (xhr.status === 409) {
						// in use
						this.__addCredential(event.creds);
					}
				}.bind(this));
			break;

			case 'DELETE_CREDENTIAL_ERROR':
				this.__addError('credentials', event.err);
			break;

			case 'CREATE_CREDENTIAL_ERROR':
				this.__addError('credentials', event.err);
			break;

			case 'CONFIRM_CLUSTER_DELETE':
				Client.deleteCluster(event.clusterID);
			break;

			case 'LAUNCH_CLUSTER_FAILURE':
				window.console.error(event);
			break;

			case 'INSTALL_PROMPT_RESPONSE':
				Client.sendPromptResponse(event.clusterID, event.promptID, event.data);
			break;

			case 'CHECK_CERT':
				cluster = this.__findCluster(event.clusterID);
				if (cluster) {
					Client.checkCert(event.domainName).then(function () {
						cluster.handleEvent({
							name: 'CERT_VERIFIED',
							clusterID: cluster.ID
						});
					}.bind(this));
				}
			break;

			case 'CREDENTIALS_CHANGE':
				newCluster.handleEvent(extend({}, event, {clusterID: 'new'}));
			break;

			case 'LIST_CLOUD_REGIONS':
				(this.__createCredPromise || Promise.resolve(null)).then(function () {
					Client.listCloudRegions(event.cloud, event.credentialID);
				});
			break;

			case 'LIST_AZURE_SUBSCRIPTIONS':
				(this.__createCredPromise || Promise.resolve(null)).then(function () {
					Client.listAzureSubscriptions(event.credentialID);
				});
			break;

			case 'CLOUD_REGIONS':
				newCluster.handleEvent(extend({}, event, {clusterID: 'new'}));
			break;

			case 'AZURE_SUBSCRIPTIONS':
				newCluster.handleEvent(extend({}, event, {clusterID: 'new'}));
			break;

			case 'CLUSTER_STATE':
				if (event.state === 'deleted') {
					this.__removeCluster(event.clusterID);
				}
			break;
		}
	},

	launchAWS: function (inputs) {
		var cluster = new Cluster({});
		cluster.type = 'aws';
		cluster.credentialID = newCluster.getInstallState().credentialID;
		cluster.region = inputs.region;
		cluster.instanceType = inputs.instanceType;
		cluster.numInstances = inputs.numInstances;

		if (inputs.vpcCidr) {
			cluster.vpcCidr = inputs.vpcCidr;
		}

		if (inputs.subnetCidr) {
			cluster.subnetCidr = inputs.subnetCidr;
		}

		Client.launchCluster(cluster.toJSON());
	},

	launchDigitalOcean: function () {
		var state = newCluster.getInstallState();
		var cluster = new Cluster({});
		cluster.type = 'digital_ocean';
		cluster.credentialID = state.credentialID;
		cluster.region = state.selectedRegionSlug;
		cluster.size = state.selectedSizeSlug;
		cluster.numInstances = state.numInstances;

		Client.launchCluster(cluster.toJSON());
	},

	launchAzure: function () {
		var state = newCluster.getInstallState();
		var cluster = new Cluster({});
		cluster.type = 'azure';
		cluster.credentialID = state.credentialID;
		cluster.region = state.selectedRegionSlug;
		cluster.size = state.selectedSizeSlug;
		cluster.numInstances = state.numInstances;
		cluster.subscriptionID = state.azureSubscriptionID;

		Client.launchCluster(cluster.toJSON());
	},

	__addError: function (type, err) {
		var errors = this.state.errors;
		errors[type] = [err].concat(errors[type]);
		this.setState({
			errors: errors
		});
	},

	__clearErrors: function (type) {
		var errors = this.state.errors;
		errors[type] = [];
		this.setState({
			errors: errors
		});
	},

	__addCredential: function (data) {
		var index = this.__findCredentialIndex(data.id);
		if (index !== -1) {
			return;
		}
		var creds = [data].concat(this.state.credentials);
		this.setState({
			credentials: creds
		});
		this.__propagateCredentialsChange();
	},

	__removeCredential: function (credentialID) {
		var creds = this.state.credentials;
		var index = this.__findCredentialIndex(credentialID);
		if (index === -1) {
			return;
		}
		creds = creds.slice(0, index).concat(creds.slice(index+1));
		this.setState({
			credentials: creds
		});
		this.__propagateCredentialsChange();
	},

	__propagateCredentialsChange: function () {
		Dispatcher.dispatch({
			name: 'CREDENTIALS_CHANGE',
			credentials: this.state.credentials
		});
	},

	__findCredentialIndex: function (credentialID) {
		var creds = this.state.credentials;
		for (var i = 0, len = creds.length; i < len; i++) {
			if (creds[i].id === credentialID) {
				return i;
			}
		}
		return -1;
	},

	__addCluster: function (cluster) {
		var index = this.__findClusterIndex(cluster.ID);
		if (index !== -1) {
			console.warn('cluster '+ cluster.ID +' already added!');
			return;
		}
		var clusters = [cluster].concat(this.state.clusters);
		var newState = {
			clusters: clusters
		};
		if (cluster.ID === this.state.currentClusterID) {
			newState.currentCluster = cluster;
		}
		this.setState(newState);
		cluster.addChangeListener(this.__handleClusterChanged);
	},

	__findClusterIndex: function (clusterID) {
		var clusters = this.state.clusters;
		for (var i = 0, len = clusters.length; i < len; i++) {
			if (clusters[i].ID === clusterID) {
				return i;
			}
		}
		return -1;
	},

	__findCluster: function (clusterID) {
		var index = this.__findClusterIndex(clusterID);
		if (index === -1) {
			return null;
		} else {
			return this.state.clusters[index];
		}
	},

	__removeCluster: function (clusterID) {
		var index = this.__findClusterIndex(clusterID);
		if (index === -1) {
			return;
		}
		var clusters = this.state.clusters;
		var cluster = clusters[index];
		clusters = clusters.slice(0, index).concat(clusters.slice(index+1));
		cluster.removeChangeListener(this.__handleClusterChanged);
		this.setState({
			clusters: clusters
		});
	},

	__handleClusterChanged: function () {
		// TODO: handle rapid fire change in chunks
		this.setState({
			clusters: this.state.clusters
		});
	}
});
