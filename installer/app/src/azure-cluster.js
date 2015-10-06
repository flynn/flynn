import { extend } from 'marbles/utils';
import BaseCluster from './base-cluster';
import Dispatcher from './dispatcher';

var AzureCluster = BaseCluster.createClass('azure', {
	mixins: [{
		ctor: {
			defaultRegionSlug: 'East US'
		}
	}],

	getInitialState: function () {
		var state = this.constructor.__super__.getInitialState.call(this);
		return extend({}, state, {
			selectedSizeSlug: null,
			azureSubscriptionID: null,
			azureSubscriptions: []
		});
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = extend(this.constructor.__super__.__computeState.call(this, attrs), {
			selectedSizeSlug: attrs.selectedSizeSlug || prevState.selectedSizeSlug,
			azureSubscriptionID: attrs.azureSubscriptionID || prevState.azureSubscriptionID,
			azureSubscriptions: attrs.azureSubscriptions || prevState.azureSubscriptions
		});

		if (state.selectedSizeSlug === null) {
			state.selectedSizeSlug = 'Standard_D1';
		}

		if (state.selectedSizeSlug === null && state.selectedRegion) {
			state.selectedSizeSlug = state.selectedRegion.sizes[0];
		}

		if (state.azureSubscriptionID === null && state.azureSubscriptions.length > 0) {
			state.azureSubscriptionID = state.azureSubscriptions[0].id;
		}

		if (state.credentialID === null && state.credentials.length > 0) {
			state.credentialID = state.credentials[0].id;
		}

		return state;
	},

	setState: function (newState, shouldDelay) {
		this.attrs.size = newState.selectedSizeSlug;
		this.attrs.subscriptionID = newState.azureSubscriptionID;

		var prevCredentialID = this.state.credentialID;
		if (newState.hasOwnProperty('credentialID') && newState.credentialID !== prevCredentialID) {
			Dispatcher.dispatch({
				name: 'LIST_AZURE_SUBSCRIPTIONS',
				credentialID: newState.credentialID
			});
			Dispatcher.dispatch({
				name: 'LIST_CLOUD_REGIONS',
				credentialID: newState.credentialID,
				cloud: newState.selectedCloud
			});
		}
		this.constructor.__super__.setState.call(this, newState, shouldDelay);
	},

	handleEvent: function (event) {
		var __super = this.constructor.__super__.handleEvent.bind(this, event);

		if (event.clusterID !== this.attrs.ID) {
			__super();
			return;
		}

		var state;
		switch (event.name) {
		case 'AZURE_SUBSCRIPTIONS':
			state = this.state;
			if (state.credentialID !== event.credentialID || state.selectedCloud !== 'azure') {
				return;
			}
			this.setState(this.__computeState({
				azureSubscriptions: event.subscriptions
			}));
			break;

		case 'SELECT_AZURE_SUBSCRIPTION':
			this.setState(this.__computeState({
				azureSubscriptionID: event.subscriptionID
			}));
			break;

		default:
			__super();
		}
	}
});

AzureCluster.jsonFields = extend({}, BaseCluster.jsonFields, {
	size: 'size',
	subscription_id: 'subscriptionID'
});

export default AzureCluster;
