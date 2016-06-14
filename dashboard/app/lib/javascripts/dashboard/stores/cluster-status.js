import Store from '../store';
import Config from '../config';

var FETCH_INTERVAL = 10000; // 10 seconds

var ClusterStatus = Store.createClass({
	displayName: 'Stores.ClusterStatus',

	getState: function () {
		return this.state;
	},

	getInitialState: function () {
		return {
			lastFetchedAt: null,
			healthy: null,
			version: null,
			services: []
		};
	},

	didBecomeActive: function () {
		this.__fetchClusterStatus().then(this.__setFetchInterval.bind(this));
	},

	didBecomeInactive: function () {
		clearTimeout(this.__fetchClusterStatusTimeout);
		this.constructor.discardInstance(this);
	},

	__setFetchInterval: function () {
		this.__fetchClusterStatusTimeout = setTimeout(function () {
			this.__fetchClusterStatus().then(this.__setFetchInterval.bind(this));
		}.bind(this), FETCH_INTERVAL);
	},

	__fetchClusterStatus: function () {
		return Config.client.getClusterStatus().then(function (status) {
			var data = status.data;
			this.setState({
				lastFetchedAt: new Date(),
				healthy: data.status === 'healthy',
				version: data.version,
				services: function (detail) {
					detail = detail || {};
					return Object.keys(detail).sort().map(function (k) {
						return {
							name: k,
							healthy: detail[k].status === 'healthy',
							version: detail[k].version
						};
					});
				}(data.detail)
			});
		}.bind(this));
	}
});

export default ClusterStatus;
