import HTTP from 'marbles/http';
import JSONMiddleware from 'marbles/http/middleware/serialize_json';
import { extend } from 'marbles/utils';
import Dispatcher from './dispatcher';
import Config from './config';

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

	launchInstall: function (data) {
		this.performRequest('POST', {
			url: Config.endpoints.install,
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		}).then(function (args) {
			Dispatcher.dispatch({
				name: 'LAUNCH_INSTALL_SUCCESS',
				res: args[0],
				xhr: args[1]
			});
		}).catch(function (args) {
			Dispatcher.dispatch({
				name: 'LAUNCH_INSTALL_FAILURE',
				res: args[0],
				xhr: args[1]
			});
		});
	},

	checkInstallExists: function (installID) {
		var handleResponse = function (args) {
			var res = args[0];
			var xhr = args[1];
			Dispatcher.dispatch({
				name: 'INSTALL_EXISTS',
				exists: xhr.status === 200,
				id: res.id
			});
		};
		this.performRequest('GET', {
			url: '/install/'+ installID,
			headers: {
				'Accept': 'application/json'
			}
		}).then(handleResponse).catch(handleResponse);
	},

	sendPromptResponse: function (promptID, data) {
		this.performRequest('POST', {
			url: Config.endpoints.prompt.replace(':id', promptID),
			body: data,
			headers: {
				'Content-Type': 'application/json'
			}
		});
	},

	checkCert: function (clusterDomain) {
		this.performRequest("GET", {
			url: "https://dashboard."+ clusterDomain +"/ping"
		}).then(function () {
			Dispatcher.dispatch({
				name: "CERT_VERIFIED"
			});
		});
	},

	openEventStream: function (installID) {
		if (this.__es && this.__es.readyState !== 2) {
			return false;
		}
		var url = Config.endpoints.events.replace(':id', installID);
		var es = this.__es = new EventSource(url);
		es.addEventListener('error', function (e) {
			window.console.error('event stream error: ', e);
			es.close();
		}, false);
		es.addEventListener('message', function (e) {
			var data = JSON.parse(e.data);
			switch (data.type) {
				case 'prompt':
					if (data.prompt.resolved) {
						Dispatcher.dispatch({
							name: 'INSTALL_PROMPT_RESOLVED',
							data: data.prompt
						});
					} else {
						Dispatcher.dispatch({
							name: 'INSTALL_PROMPT_REQUESTED',
							data: data.prompt
						});
					}
				break;

				case 'done':
					Dispatcher.dispatch({
						name: 'INSTALL_DONE'
					});
					es.close();
				break;

				case 'domain':
					Dispatcher.dispatch({
						name: 'DOMAIN',
						domain: data.description
					});
				break;

				case 'dashboard_login_token':
					Dispatcher.dispatch({
						name: 'DASHBOARD_LOGIN_TOKEN',
						token: data.description
					});
				break;

				case 'ca_cert':
					Dispatcher.dispatch({
						name: 'CA_CERT',
						cert: data.description
					});
				break;

				default:
					Dispatcher.dispatch({
						name: 'INSTALL_EVENT',
						data: data
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
