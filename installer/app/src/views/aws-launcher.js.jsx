import { green as GreenBtnCSS, disabled as BtnDisabledCSS } from './css/button';
import AWSRegionPicker from './aws-region-picker';
import AWSInstanceTypePicker from './aws-instance-type-picker';
import AWSAdvancedOptions from './aws-advanced-options';
import IntegerPicker from './integer-picker';
import Dispatcher from '../dispatcher';
import { extend } from 'marbles/utils';

var InstallConfig = React.createClass({
	render: function () {
		return (
			<form onSubmit={this.__handleSubmit}>
				<div>
					<br />
					<br />
					<AWSRegionPicker
						value={this.state.region}
						onChange={this.__handleRegionChange} />
					<br />
					<br />
					<AWSInstanceTypePicker
						value={this.state.instanceType}
						onChange={this.__handleInstanceTypeChange} />
					<br />
					<br />
					<label>
						<div>Number of instances: </div>
						<div style={{
							width: 60
							}}>
							<IntegerPicker
								minValue={1}
								maxValue={5}
								skipValues={[2]}
								value={this.state.numInstances}
								onChange={this.__handleNumInstancesChange} />
						</div>
					</label>
					<br />
					<br />
					<AWSAdvancedOptions onChange={this.__handleAdvancedOptionsChange}/>
					<br />
					<br />
					<button
						type="submit"
						style={extend({}, GreenBtnCSS,
							this.state.launchBtnDisabled ? BtnDisabledCSS : {})}
						disabled={this.state.launchBtnDisabled}>Launch</button>
				</div>
			</form>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	__getState: function () {
		var state = this.props.state;
		return {
			credentialID: state.credentialID,
			region: 'us-east-1',
			instanceType: 'm3.medium',
			numInstances: 1,
			advancedOptionsKeys: [],
			launchBtnDisabled: state.currentStep !== 'configure'
		};
	},

	__handleRegionChange: function (region) {
		this.setState({
			region: region
		});
	},

	__handleInstanceTypeChange: function (instanceType) {
		this.setState({
			instanceType: instanceType
		});
	},

	__handleNumInstancesChange: function (numInstances) {
		this.setState({
			numInstances: numInstances
		});
	},

	__handleAdvancedOptionsChange: function (values) {
		this.setState(extend({}, values, {
			advancedOptionsKeys: Object.keys(values)
		}));
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		this.setState({
			launchBtnDisabled: true
		});
		var advancedOptions = {};
		this.state.advancedOptionsKeys.forEach(function (key) {
			advancedOptions[key] = this.state[key];
		}.bind(this));
		Dispatcher.dispatch(extend({
			name: 'LAUNCH_AWS',
			region: this.state.region,
			instanceType: this.state.instanceType,
			numInstances: this.state.numInstances
		}, advancedOptions));
	}
});
export default InstallConfig;
