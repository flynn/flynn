import { createClass } from 'marbles/utils';
import State from 'marbles/state';
import Client from './client';
import Cluster from './cluster';

var newCluster = new Cluster({id: 'new'});

export default createClass({
	mixins: [State],

	registerWithDispatcher: function (dispatcher) {
		this.dispatcherIndex = dispatcher.register(this.handleEvent.bind(this));
	},

	willInitialize: function () {
		this.__handleClusterChanged = this.__handleClusterChanged.bind(this);
		this.state = this.getInitialState();
		this.__changeListeners = [];
	},

	getInitialState: function () {
		return {
			clusters: [],
			currentClusterID: null,
			currentCluster: newCluster
		};
	},

	handleEvent: function (event) {
		var cluster;
		switch (event.name) {
			case 'LAUNCH_AWS':
				this.launchAWS(event);
			break;

			case 'NEW_CLUSTER':
				this.__addCluster(event.cluster);
			break;

			case 'CURRENT_CLUSTER':
				this.setState({
					currentClusterID: event.clusterID,
					currentCluster: event.clusterID === null ? newCluster : this.__findCluster(event.clusterID)
				});
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
							name: 'CERT_VERIFIED'
						});
					}.bind(this));
				}
			break;

			default:
				if (event.name === "CLUSTER_STATE" && event.state === "deleted") {
					this.__removeCluster(event.clusterID);
				}

				cluster = this.__findCluster(event.clusterID);
				if (cluster) {
					cluster.handleEvent(event);
				}
			break;
		}
	},

	launchAWS: function (inputs) {
		var cluster = new Cluster({});
		cluster.creds = inputs.creds;
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
