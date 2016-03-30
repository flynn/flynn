import { createClass, extend } from 'marbles/utils';
import State from 'marbles/state';
import Client from './client';
import Cluster from './cluster';
import Dispatcher from './dispatcher';

var DEFAULT_CLOUD = 'aws';
var newCluster = Cluster.newOfType(DEFAULT_CLOUD, {id: 'new'});

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
			},
			prompts: {},
			credentialsPromptValue: {},
			progressMeters: {}
		};
	},

	handleEvent: function (event) {
		var cluster;
		switch (event.name) {
		case 'SELECT_CLOUD':
			if (newCluster.constructor.type !== event.cloud) {
				this.__setNewCluster(event.cloud);
				this.setState({
					currentCloudSlug: event.cloud,
					currentCluster: newCluster
				});
			}
			break;

		case 'LAUNCH_CLUSTER':
			Client.launchCluster(newCluster.toJSON(), newCluster.getBackupFile());
			break;

		case 'LAUNCH_CLUSTER_SUCCESS':
			this.__setNewCluster(newCluster.type);
			break;

		case 'NEW_CLUSTER':
			this.__addCluster(event.cluster);
			if (this.__pendingCurrentClusterID === event.cluster.attrs.ID) {
				this.__pendingCurrentClusterID = null;
				this.setState({
					currentClusterID: event.cluster.attrs.ID,
					currentCluster: event.cluster,
					currentCloudSlug: event.cluster.type
				});
			}
			break;

		case 'CLUSTER_UPDATE':
			this.__replaceCluster(event.cluster);
			break;

		case 'NEW_CREDENTIAL':
			this.__addCredential(event.credential);
			break;

		case 'CREDENTIAL_DELETED':
			this.__removeCredential(event.id);
			break;

		case 'PROMPT_SELECT_CREDENTIAL':
			this.setState({
				credentialsPromptValue: (function (credentialsPromptValue) {
					var item = {};
					item[event.clusterID] = event.credentialID;
					return extend({}, credentialsPromptValue, item);
				})(this.state.credentialsPromptValue)
			});
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

		case 'INSTALL_PROMPT_REQUESTED':
			this.setState({
				prompts: (function () {
					var prompts = extend({}, this.state.prompts);
					prompts[event.clusterID] = event.prompt;
					return prompts;
				}.bind(this))()
			});
			break;

		case 'INSTALL_PROMPT_RESOLVED':
			this.setState({
				prompts: (function () {
					var prompts = extend({}, this.state.prompts);
					if ((prompts[event.clusterID] || {}).id === event.prompt.id) {
						delete prompts[event.clusterID];
					}
					return prompts;
				}.bind(this))()
			});
			break;

		case 'INSTALL_PROMPT_RESPONSE':
			Client.sendPromptResponse(event.clusterID, event.promptID, event.data);
			break;

		case 'PROGRESS':
			this.setState({
				progressMeters: (function (progressMeters) {
					var clusterID = event.clusterID;
					progressMeters[clusterID] = progressMeters[clusterID] || {};
					progressMeters[clusterID][event.data.id] = event.data;
					return progressMeters;
				})(extend({}, this.state.progressMeters))
			});
			break;

		case 'CHECK_CERT':
			cluster = this.__findCluster(event.clusterID);
			if (cluster) {
				Client.checkCert(event.domainName).then(function () {
					cluster.handleEvent({
						name: 'CERT_VERIFIED',
						clusterID: cluster.attrs.ID
					});
				}.bind(this));
			}
			break;

		case 'CREDENTIALS_CHANGE':
			newCluster.handleEvent(extend({}, event, {clusterID: 'new'}));
			this.state.clusters.forEach(function (c) {
				c.setCredentials(event.credentials);
			});
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
			if (event.state !== 'starting') {
				this.setState({
					progressMeters: (function (progressMeters) {
						var clusterID = event.clusterID;
						if (progressMeters.hasOwnProperty(clusterID)) {
							delete progressMeters[clusterID];
						}
						return progressMeters;
					})(extend({}, this.state.progressMeters))
				});
			}
			break;
		}
	},

	__setNewCluster: function (t) {
		newCluster.removeChangeListener(this.__handleClusterChanged);
		newCluster = Cluster.newOfType(t, {id: 'new', credentials: this.state.credentials});
		newCluster.addChangeListener(this.__handleClusterChanged);
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
		var index = this.__findClusterIndex(cluster.attrs.ID);
		if (index !== -1) {
			window.console.warn('cluster '+ cluster.attrs.ID +' already added!');
			return;
		}
		var clusters = [cluster].concat(this.state.clusters);
		var newState = {
			clusters: clusters
		};
		cluster.setCredentials(this.state.credentials);
		if (cluster.attrs.ID === this.state.currentClusterID) {
			newState.currentCluster = cluster;
		}
		this.setState(newState);
		cluster.addChangeListener(this.__handleClusterChanged);
	},

	__replaceCluster: function (attrs) {
		var cluster = this.__findCluster(attrs.id);
		if (cluster === null) {
			window.console.warn('cluster '+ cluster.id +' not found!');
			return;
		}
		cluster.setAttrs(attrs);
	},

	__findClusterIndex: function (clusterID) {
		var clusters = this.state.clusters;
		for (var i = 0, len = clusters.length; i < len; i++) {
			if (clusters[i].attrs.ID === clusterID) {
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
