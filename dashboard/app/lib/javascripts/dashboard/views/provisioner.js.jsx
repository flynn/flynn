import Config from 'dashboard/config';
import Dispatcher from 'dashboard/dispatcher';
import ProvidersStore from 'dashboard/stores/providers';

var providersStoreID = 'default';

var providerAttrs = Config.PROVIDER_ATTRS;

var Provisioner = React.createClass({
	displayName: "Views.Providers",

	render: function () {
		var providers = this.state.providers;
		var newResourceStates = this.state.newResourceStates;

		return (
			<ul style={{
				display: 'flex',
				listStyle: 'none',
				padding: 0,
				margin: 0
			}}>
				{providers.map(function (provider) {
					var attrs = providerAttrs[provider.name] || {
						title: provider.name,
						img: 'about:blank'
					};
					var newResourceState = newResourceStates[provider.id] || {};
					return (
						<li key={provider.id} className='panel' style={{
							padding: '1rem',
							paddingBottom: '2rem',
							marginRight: '1rem',
							textAlign: 'center',
							minWidth: 200
						}}>
							<div style={{
								display: 'table',
								margin: '0 auto',
								marginBottom: '0.25rem'
							}}>
								<img src={attrs.img} style={{
									display: 'table-cell',
									maxWidth: '100%',
									maxHeight: '100px',
									width: '100%',
									verticalAlign: 'middle'
								}} />
							</div>
							<h3>{attrs.title}</h3>
							{newResourceState.errMsg ? (
								<div className='alert-error' style={{
									margin: '0.25rem'
								}}>{newResourceState.errMsg}</div>
							) : null}
							<div
								className='btn-green'
								onClick={this.__handleProvisionBtnClick.bind(this, provider.id)}
								disabled={newResourceState.isCreating}
								style={{marginBottom: '1rem'}}>{newResourceState.isCreating ? 'Please wait...' : 'Provision'}</div>
						</li>
					);
				}, this)}
			</ul>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		ProvidersStore.addChangeListener(providersStoreID, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		ProvidersStore.removeChangeListener(providersStoreID, this.__handleStoreChange);
	},

	__handleProvisionBtnClick: function (providerID, e) {
		e.preventDefault();
		var newResourceState = this.state.newResourceStates[providerID] || {};
		if (newResourceState.isCreating) {
			return;
		}
		Dispatcher.dispatch({
			name: 'PROVISION_RESOURCE_WITH_ROUTE',
			providerID: providerID,
			resourceID: this.props.resourceID
		});
	},

	__getState: function () {
		var state = {};

		var providersState = ProvidersStore.getState(providersStoreID);
		state.providers = providersState.providers;
		state.newResourceStates = providersState.newResourceStates;

		return state;
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props));
	}
});

export default Provisioner;
