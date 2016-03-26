import Config from 'dashboard/config';
import ProvidersStore from 'dashboard/stores/providers';

var providersStoreID = 'default';

var providerAttrs = Config.PROVIDER_ATTRS;

var Provisioner = React.createClass({
	displayName: "Views.Providers",

	render: function () {
		var providers = this.state.providers;
		var providerSelection = this.state.providerSelection;
		var disabledProviderIDs = this.props.disabledProviderIDs || [];

		return (
			<ul className="provider-picker">
				{providers.map(function (provider) {
					var attrs = providerAttrs[provider.name] || {
						title: provider.name,
						img: 'about:blank'
					};
					return (
						<li
							key={provider.id}
							className={'panel'+ (providerSelection[provider.id] === true ? ' selected' : '') + (disabledProviderIDs.indexOf(provider.id) !== -1 ? ' disabled' : '')}
							onClick={this.__handleProviderClick.bind(this, provider.id)}>
							<img src={attrs.img} />
							<h3>{attrs.title}</h3>
						</li>
					);
				}, this)}
			</ul>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props, {});
	},

	componentDidMount: function () {
		ProvidersStore.addChangeListener(providersStoreID, this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		ProvidersStore.removeChangeListener(providersStoreID, this.__handleStoreChange);
	},

	__handleProviderClick: function (providerID, e) {
		e.preventDefault();
		if ((this.props.disabledProviderIDs || []).indexOf(providerID) !== -1) {
			return;
		}
		var providerSelection = this.state.providerSelection || {};
		if (providerSelection[providerID]) {
			providerSelection[providerID] = false;
		} else {
			providerSelection[providerID] = true;
		}
		this.setState(this.__getState(this.props, this.state, providerSelection));
		var selectedProviderIDs = [];
		for (var k in providerSelection) {
			if (providerSelection.hasOwnProperty(k) && providerSelection[k] === true) {
				selectedProviderIDs.push(k);
			}
		}
		this.props.onChange(selectedProviderIDs);
	},

	__getState: function (props, prevState, providerSelection) {
		var state = {};

		var providersState = ProvidersStore.getState(providersStoreID);
		state.providers = providersState.providers;
		state.providerSelection = providerSelection === undefined ? prevState.providerSelection || {} : providerSelection;

		return state;
	},

	__handleStoreChange: function () {
		this.setState(this.__getState(this.props, this.state));
	}
});

export default Provisioner;
