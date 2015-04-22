import HTTP from 'marbles/http';
import JSONMiddleware from 'marbles/http/middleware/serialize_json';
import { extend } from 'marbles/utils';
import Dispatcher from './dispatcher';
import Config from './config';
import Cluster from './cluster';

var Client = {
	performRequest: function (method, args) {
		if ( !args.url ) {
			return Promise.reject(new Error("Client: Can't make request without URL"));
		}

		return HTTP(extend({
			method: method,
			middleware: [
				JSONMiddleware
			]
		}, args)).then(function (args) {
			var res = args[0];
			var xhr = args[1];
			return new Promise(function (resolve, reject) {
				if (xhr.status >= 200 && xhr.status < 400) {
					resolve([res, xhr]);
				} else {
					reject([res, xhr]);
				}
			});
		});
	},

	launchCluster: function (data) {
		this.performRequest('POST', {
			url: Config.endpoints.clusters,
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		}).then(function (args) {
			Dispatcher.dispatch({
				name: 'LAUNCH_CLUSTER_SUCCESS',
				clusterID: args[0].id,
				res: args[0],
				xhr: args[1]
			});
		}).catch(function (args) {
			Dispatcher.dispatch({
				name: 'LAUNCH_CLUSTER_FAILURE',
				res: args[0],
				xhr: args[1]
			});
		});
	},

	deleteCluster: function (clusterID) {
		return this.performRequest('DELETE', {
			url: Config.endpoints.cluster.replace(':id', clusterID)
		});
	},

	sendPromptResponse: function (clusterID, promptID, data) {
		this.performRequest('POST', {
			url: Config.endpoints.prompt.replace(':id', clusterID).replace(':prompt_id', promptID),
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	checkCert: function (clusterDomain) {
		return this.performRequest("GET", {
			url: "https://dashboard."+ clusterDomain +"/ping"
		});
	},

	openEventStream: function () {
		if (this.__es && this.__es.readyState !== 2) {
			return false;
		}
		var es = this.__es = new EventSource(Config.endpoints.events);
		es.addEventListener('error', function (e) {
			window.console.error('event stream error: ', e);
			es.close();
		}, false);
		es.addEventListener('message', function (e) {
			var data = JSON.parse(e.data);
			var event = {};
			if (data.cluster_id !== undefined) {
				event.clusterID = data.cluster_id;
			}
			switch (data.type) {
				case 'new_cluster':
					event.name = 'NEW_CLUSTER';
					event.cluster = new Cluster(data.cluster);
				break;

				case 'cluster_state':
					event.name = 'CLUSTER_STATE';
					event.state = data.description;
				break;

				case 'prompt':
					event.prompt = data.prompt;
					if (data.prompt.resolved) {
						event.name = 'INSTALL_PROMPT_RESOLVED';
					} else {
						event.name = 'INSTALL_PROMPT_REQUESTED';
					}
				break;

				case 'install_done':
					event.name = 'INSTALL_DONE';
					event.cluster = data.cluster;
				break;

				case 'error':
					event.name = 'INSTALL_ERROR';
					event.message = data.description;
				break;

				case 'install_log':
					event.name = 'INSTALL_LOG';
					event.data = data;
				break;

				default:
					event.name = 'DEFAULT_EVENT';
					event.data = data;
			}
			Dispatcher.dispatch(event);
			if (data.type === 'install_done') {
				Dispatcher.dispatch({
					name: 'CHECK_CERT',
					clusterID: event.clusterID,
					domainName: data.cluster.domain.domain
				});
			}
		}, false);
		return true;
	},

	closeEventStream: function () {
		if (this.__es && this.__es.readyState !== 2) {
			this.__es.close();
		}
	}
};

export default Client;
