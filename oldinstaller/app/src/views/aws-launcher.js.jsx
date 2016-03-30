import { green as GreenBtnCSS, disabled as BtnDisabledCSS } from './css/button';
import AWSRegionPicker from './aws-region-picker';
import AWSInstanceTypePicker from './aws-instance-type-picker';
import AWSAdvancedOptions from './aws-advanced-options';
import IntegerPicker from './integer-picker';
import Dispatcher from '../dispatcher';
import { extend } from 'marbles/utils';

var InstallConfig = React.createClass({
	render: function () {
		var clusterState = this.props.state;

		var launchBtnDisabled = clusterState.currentStep !== 'configure';
		launchBtnDisabled = launchBtnDisabled || !clusterState.credentialID;
		launchBtnDisabled = launchBtnDisabled || !clusterState.selectedRegionSlug;

		return (
			<form onSubmit={this.__handleSubmit}>
				<div>
					<br />
					<br />
					<AWSRegionPicker
						value={clusterState.selectedRegionSlug}
						onChange={this.__handleRegionChange} />
					<br />
					<br />
					<AWSInstanceTypePicker
						value={clusterState.selectedInstanceType}
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
								skipValues={[2]}
								value={clusterState.numInstances}
								onChange={this.__handleNumInstancesChange} />
						</div>
					</label>
					<br />
					<br />
					<AWSAdvancedOptions
						state={this.props.state}
						onChange={this.__handleAdvancedOptionsChange}/>
					<br />
					<br />
					<button
						type="submit"
						style={extend({}, GreenBtnCSS, launchBtnDisabled ? BtnDisabledCSS : {})}
						disabled={launchBtnDisabled}>Launch</button>
				</div>
			</form>
		);
	},

	__handleRegionChange: function (region) {
		Dispatcher.dispatch({
			name: 'SELECT_REGION',
			region: region,
			clusterID: 'new'
		});
	},

	__handleInstanceTypeChange: function (instanceType) {
		Dispatcher.dispatch({
			name: 'SELECT_AWS_INSTANCE_TYPE',
			instanceType: instanceType,
			clusterID: 'new'
		});
	},

	__handleNumInstancesChange: function (numInstances) {
		Dispatcher.dispatch({
			name: 'SELECT_NUM_INSTANCES',
			numInstances: numInstances,
			clusterID: 'new'
		});
	},

	__handleAdvancedOptionsChange: function (values) {
		Dispatcher.dispatch(extend({
			name: 'SET_AWS_OPTIONS',
			clusterID: 'new'
		}, values));
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		this.setState({
			launchBtnDisabled: true
		});
		Dispatcher.dispatch({
			name: 'LAUNCH_CLUSTER'
		});
	}
});
export default InstallConfig;
