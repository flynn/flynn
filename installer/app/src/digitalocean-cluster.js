import { extend } from 'marbles/utils';
import BaseCluster from './base-cluster';
import Dispatcher from './dispatcher';

var DigitalOceanCluster = BaseCluster.createClass('digital_ocean', {
	mixins: [{
		ctor: {
			defaultRegionSlug: 'nyc3'
		}
	}],

	getInitialState: function () {
		var state = this.constructor.__super__.getInitialState.call(this);
		return extend({}, state, {
			selectedSizeSlug: null
		});
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = extend(this.constructor.__super__.__computeState.call(this, attrs), {
			selectedSizeSlug: attrs.selectedSizeSlug || prevState.selectedSizeSlug
		});

		if (state.selectedSizeSlug === null) {
			state.selectedSizeSlug = '2gb';
		}

		if (state.selectedSizeSlug === null && state.selectedRegion) {
			state.selectedSizeSlug = state.selectedRegion.sizes[0];
		}

		if (state.credentialID === null && state.credentials.length > 0) {
			state.credentialID = state.credentials[0].id;
		}

		return state;
	},

	setState: function (newState, shouldDelay) {
		this.attrs.size = newState.selectedSizeSlug;

		var prevCredentialID = this.state.credentialID;
		if (newState.hasOwnProperty('credentialID') && newState.credentialID !== prevCredentialID) {
			Dispatcher.dispatch({
				name: 'LIST_CLOUD_REGIONS',
				credentialID: newState.credentialID,
				cloud: newState.selectedCloud
			});
		}
		this.constructor.__super__.setState.call(this, newState, shouldDelay);
	}
});

DigitalOceanCluster.jsonFields = extend({}, BaseCluster.jsonFields, {
	size: 'size'
});

export default DigitalOceanCluster;
