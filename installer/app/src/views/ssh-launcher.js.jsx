import { extend } from 'marbles/utils';
import Dispatcher from '../dispatcher';
import IntegerPicker from './integer-picker';
import AdvancedOptions from './advanced-options';
import { green as GreenBtnCSS, disabled as DisabledBtnCSS } from './css/button';
import Sheet from './css/sheet';
import Colors from './css/colors';

var InstallConfig = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			marginTop: '1rem',
			selectors: [
				['> label', {
					display: 'block',
					selectors: [
						['> span:after', {
							display: 'block',
							content: '" "'
						}]
					]
				}],

				['> * + *', {
					marginTop: '1rem'
				}],

				['input[type=text]', {
					padding: '3px',
					border: '1px solid '+ Colors.grayBlueColor
				}],

				['button[type=submit]', GreenBtnCSS],
				['button[type=submit][disabled]', DisabledBtnCSS]
			]
		});

		return {
			styleEl: styleEl
		};
	},

	render: function () {
		var clusterState = this.props.state;

		var targets = clusterState.targets;

		var launchBtnDisabled = clusterState.currentStep !== 'configure';
		if (!launchBtnDisabled) {
			targets.forEach(function (t) {
				if (t.isDup || !t.user || !t.ip || !t.port) {
					launchBtnDisabled = true;
				}
			});
		}

		return (
			<form id={this.state.styleEl.id} onSubmit={this.__handleSubmit}>
				<p>The SSH installer targets hosts that already have Ubuntu 14.04 installed.</p>

				<p>Ensure all UDP and TCP traffic is open between all hosts, and the following ports are open externally:</p>

				<ul>
						<li>80 (HTTP)</li>
						<li>443 (HTTPS)</li>
						<li>3000 to 3500 (user-defined TCP services, optional)</li>
				</ul>

				<label>
					<span>Number of instances:</span>
					<div style={{
						width: 60
					}}>
						<IntegerPicker
							minValue={1}
							skipValues={[2]}
							value={clusterState.numInstances}
							onChange={this.__handleNumInstancesChange} />
					</div>
				</label>

				{targets.map(function (t, index) {
					return (
						<div key={index} style={{
							display: 'flex'
						}}>
							<label>
								<input type="text" value={t.user || ''} onChange={function (e) {
									var newValue = e.target.value;
									this.__handleTargetUserChange(newValue, index);
								}.bind(this)} placeholder="ubuntu" />
							</label>
							<span>@</span>
							<label>
								<input type="text" value={t.ip || ''} onChange={function (e) {
									var newValue = e.target.value;
									this.__handleTargetIPChange(newValue, index);
								}.bind(this)} placeholder="xx.xxx.xx.xx" style={t.isDup ? {
									border: '1px solid'+ Colors.redColor
								} : null} />
							</label>
							<span>:</span>
							<label>
								<input type="text" value={t.port || ''} onChange={function (e) {
									var newValue = e.target.value;
									this.__handleTargetPortChange(newValue, index);
								}.bind(this)} placeholder="22" />
							</label>
						</div>
					);
				}.bind(this))}

				<AdvancedOptions state={clusterState} />

				<button type="submit" disabled={launchBtnDisabled}>Launch</button>
			</form>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
	},

	__handleNumInstancesChange: function (numInstances) {
		Dispatcher.dispatch({
			name: 'SELECT_NUM_INSTANCES',
			numInstances: numInstances,
			clusterID: 'new'
		});
	},

	__handleTargetUserChange: function (newValue, index) {
		var targets = [].concat(this.props.state.targets);
		targets[index] = extend({}, targets[index] || {}, {
			user: newValue
		});
		Dispatcher.dispatch({
			name: 'SET_TARGETS',
			targets: targets,
			clusterID: 'new'
		});
	},

	__handleTargetIPChange: function (newValue, index) {
		var targets = [].concat(this.props.state.targets);
		targets[index] = extend({}, targets[index] || {}, {
			ip: newValue
		});
		Dispatcher.dispatch({
			name: 'SET_TARGETS',
			targets: targets,
			clusterID: 'new'
		});
	},

	__handleTargetPortChange: function (newValue, index) {
		var targets = [].concat(this.props.state.targets);
		targets[index] = extend({}, targets[index] || {}, {
			port: newValue
		});
		Dispatcher.dispatch({
			name: 'SET_TARGETS',
			targets: targets,
			clusterID: 'new'
		});
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'LAUNCH_CLUSTER'
		});
	}
});
export default InstallConfig;
