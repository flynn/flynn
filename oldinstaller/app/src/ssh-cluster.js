import { extend } from 'marbles/utils';
import BaseCluster from './base-cluster';

var defaultTarget = {
	user: 'ubuntu',
	ip: '',
	port: '22'
};

var SSHCluster = BaseCluster.createClass('ssh', {
	mixins: [{
		ctor: {
			defaultRegionSlug: null
		}
	}],

	getInitialState: function () {
		var state = this.constructor.__super__.getInitialState.call(this);
		return extend({}, state, {
			targets: [defaultTarget]
		});
	},

	__computeState: function (attrs) {
		var prevState = this.state;
		var state = extend(this.constructor.__super__.__computeState.call(this, attrs), {
			targets: attrs.targets || prevState.targets
		});

		var numInstances = state.numInstances;
		var targets = new Array(numInstances);
		var target;
		var firstTarget = prevState.targets[0] || defaultTarget;
		var targetIPs = new Array(numInstances);
		for (var i = 0; i < numInstances; i++) {
			target = state.targets[i] || extend({}, defaultTarget);
			if (i > 0 && ((target.user === defaultTarget.user && target.port === defaultTarget.port) || (target.user === firstTarget.user && target.port === firstTarget.port))) {
				target.user = (targets[0] || firstTarget).user;
				target.port = (targets[0] || firstTarget).port;
			}
			if (target.ip !== '' && targetIPs.indexOf(target.ip) !== -1) {
				target.isDup = true;
			} else {
				target.isDup = false;
			}
			target.user = target.user.replace(/\s/g, '');
			target.ip = target.ip.replace(/\s/g, '');
			target.port = target.port.replace(/\s/g, '');
			targetIPs[i] = target.ip;
			targets[i] = target;
		}
		state.targets = targets;

		return state;
	},

	setState: function (newState, shouldDelay) {
		this.attrs.targets = newState.targets;
		this.constructor.__super__.setState.call(this, newState, shouldDelay);
	},

	toJSON: function () {
		var data = this.constructor.__super__.toJSON.apply(this, arguments);
		return data;
	},

	handleEvent: function (event) {
		var __super = this.constructor.__super__.handleEvent.bind(this, event);

		if (event.clusterID !== this.attrs.ID) {
			__super();
			return;
		}

		switch (event.name) {
		case 'SELECT_NUM_INSTANCES':
			this.setState(this.__computeState({
				num_instances: event.numInstances
			}));
			break;

		case 'SET_TARGETS':
			this.setState(this.__computeState({ targets: event.targets }));
			break;

		default:
			__super();
		}
	}
});

SSHCluster.jsonFields = extend({}, BaseCluster.jsonFields, {
	targets: 'targets'
});

export default SSHCluster;
