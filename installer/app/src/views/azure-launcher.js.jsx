import Dispatcher from '../dispatcher';
import PrettySelect from './pretty-select';
import IntegerPicker from './integer-picker';
import ExternalLink from './external-link';
import AdvancedOptions from './advanced-options';
import { green as GreenBtnCSS, disabled as DisabledBtnCSS } from './css/button';
import Sheet from './css/sheet';

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
		var sizes = ["Standard_D1","Standard_D11","Standard_D12","Standard_D13","Standard_D14","Standard_D2","Standard_D3","Standard_D4"];

		var subscriptions = clusterState.azureSubscriptions;

		var launchBtnDisabled = clusterState.currentStep !== 'configure';
		launchBtnDisabled = launchBtnDisabled || !clusterState.credentialID;
		launchBtnDisabled = launchBtnDisabled || !clusterState.selectedRegionSlug;
		launchBtnDisabled = launchBtnDisabled || !clusterState.selectedSizeSlug;
		launchBtnDisabled = launchBtnDisabled || !clusterState.azureSubscriptionID;
		launchBtnDisabled = launchBtnDisabled || clusterState.regions.length === 0;
		launchBtnDisabled = launchBtnDisabled || sizes.length === 0;
		launchBtnDisabled = launchBtnDisabled || subscriptions.length === 0;

		return (
			<form id={this.state.styleEl.id} onSubmit={this.__handleSubmit}>
				<label>
					<span>Subscription:</span>
					<PrettySelect value={clusterState.azureSubscriptionID} onChange={this.__handleSubscriptionChange}>
						{subscriptions.map(function (subscription) {
							return (
								<option key={subscription.id} value={subscription.id}>{subscription.name}</option>
							);
						})}
					</PrettySelect>
				</label>

				<label>
					<span>Region:</span>
					<PrettySelect value={clusterState.selectedRegionSlug} onChange={this.__handleRegionChange}>
						{clusterState.regions.map(function (region) {
							return (
								<option key={region.slug} value={region.slug}>{region.name}</option>
							);
						})}
					</PrettySelect>
				</label>

				<label>
					<span>Size: (See <ExternalLink href="https://azure.microsoft.com/en-us/documentation/articles/virtual-machines-size-specs/">here</ExternalLink> for details)</span>
					<PrettySelect value={clusterState.selectedSizeSlug} onChange={this.__handleSizeChange}>
						{sizes.map(function (size) {
							return (
								<option key={size} value={size}>{size}</option>
							);
						})}
					</PrettySelect>
				</label>

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

				<AdvancedOptions state={clusterState} />

				<button type="submit" disabled={launchBtnDisabled}>Launch</button>
			</form>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
	},

	__handleSubscriptionChange: function (e) {
		var subscriptionID = e.target.value;
		Dispatcher.dispatch({
			name: 'SELECT_AZURE_SUBSCRIPTION',
			subscriptionID: subscriptionID,
			clusterID: 'new'
		});
	},

	__handleRegionChange: function (e) {
		var slug = e.target.value;
		Dispatcher.dispatch({
			name: 'SELECT_REGION',
			region: slug,
			clusterID: 'new'
		});
	},

	__handleSizeChange: function (e) {
		var slug = e.target.value;
		Dispatcher.dispatch({
			name: 'SELECT_SIZE',
			slug: slug,
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

	__handleSubmit: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'LAUNCH_CLUSTER'
		});
	}
});
export default InstallConfig;
