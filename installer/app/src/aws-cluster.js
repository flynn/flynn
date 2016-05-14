import { extend } from 'marbles/utils';
import BaseCluster from './base-cluster';
import Config from './config';

var AWSCluster = BaseCluster.createClass('aws', {
	mixins: [{
		ctor: {
			defaultRegionSlug: 'us-east-1'
		}
	}],

	getInitialState: function () {
		var state = this.constructor.__super__.getInitialState.call(this);
		return extend({}, state, {
			selectedInstanceType: 'm4.large',
			vpcCidr: null,
			subnetCidr: null
		});
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = extend(this.constructor.__super__.__computeState.call(this, attrs), {
			selectedInstanceType: attrs.selectedInstanceType || prevState.selectedInstanceType,
			vpcCidr: attrs.vpcCidr || prevState.vpcCidr || null,
			subnetCidr: attrs.subnetCidr || prevState.subnetCidr || null
		});

		if (state.credentialID === null && Config.has_aws_env_credentials) {
			state.credentialID = 'aws_env';
		}

		if (state.credentialID === null && state.credentials.length > 0) {
			state.credentialID = state.credentials[0].id;
		}

		return state;
	},

	setState: function (newState, shouldDelay) {
		this.attrs.instanceType = newState.selectedInstanceType;
		this.attrs.vpcCidr = newState.vpcCidr;
		this.attrs.subnetCidr = newState.subnetCidr;
		this.constructor.__super__.setState.call(this, newState, shouldDelay);
	},

	toJSON: function () {
		var data = this.constructor.__super__.toJSON.apply(this, arguments);
		if (data.vpc_cidr === null) {
			delete data.vpc_cidr;
		}
		if (data.subnet_cidr === null) {
			delete data.subnet_cidr;
		}
		return data;
	},

	handleEvent: function (event) {
		var __super = this.constructor.__super__.handleEvent.bind(this, event);

		if (event.clusterID !== this.attrs.ID) {
			__super();
			return;
		}

		switch (event.name) {
		case 'SET_AWS_OPTIONS':
			this.setState(
				this.__computeState({
					vpcCidr: event.vpcCidr,
					subnetCidr: event.subnetCidr
				})
			);
			break;

		case 'SELECT_AWS_INSTANCE_TYPE':
			this.setState(
				this.__computeState({selectedInstanceType: event.instanceType})
			);
			break;

		default:
			__super();
		}
	}
});

AWSCluster.jsonFields = extend({}, BaseCluster.jsonFields, {
	instance_type: 'instanceType',
	vpc_cidr: 'vpcCidr',
	subnet_cidr: 'subnetCidr'
});

export default AWSCluster;
