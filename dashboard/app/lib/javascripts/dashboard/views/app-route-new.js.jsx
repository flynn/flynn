import { extend } from 'marbles/utils';
import Modal from 'Modal';
import Dispatcher from 'dashboard/dispatcher';
import NewAppRouteStore from 'dashboard/stores/app-route-new';

var NewAppRoute = React.createClass({
	displayName: "Views.NewAppRoute",

	render: function () {
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-route-new">
					<header>
						<h1>New Domain</h1>
					</header>

					<form onSubmit={this.__handleFormSubmit}>
						<div className="alert-info">Remember to change your DNS</div>

						{this.state.errorMsg ? (
							<div className="alert-error">{this.state.errorMsg}</div>
						) : null}

						<label>
							<div className="name">Domain</div>
							<input
								type="text"
								ref="domain"
								value={this.state.domain}
								disabled={this.state.isCreating === true}
								onChange={this.__handleDomainChange} />
						</label>

						<button type="submit" className="create-btn" disabled={ !this.state.domain || this.state.isCreating }>{this.state.isCreating ? "Please wait..." : "Add Domain"}</button>
					</form>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		NewAppRouteStore.addChangeListener(this.__getStoreID(this.props), this.__handleStoreChange);
		this.refs.domain.getDOMNode().focus();
	},

	componentWillUnmount: function () {
		NewAppRouteStore.removeChangeListener(this.__getStoreID(this.props), this.__handleStoreChange);
	},

	__handleStoreChange: function () {
		if (this.isMounted()) {
			this.setState(this.__getState(this.props));
		}
	},

	__getStoreID: function (props) {
		return {
			appID: props.appId
		};
	},

	__getState: function (props) {
		var prevState = this.state || {};
		var state = extend({
			domain: prevState.domain || null
		}, NewAppRouteStore.getState(this.__getStoreID(props)));
		return state;
	},

	__handleDomainChange: function (e) {
		var domain = e.target.value.trim();
		this.setState({
			domain: domain
		});
	},

	__handleFormSubmit: function (e) {
		e.preventDefault();
		var uri = document.createElement('a');
		uri.href = 'http://'+ this.state.domain;
		Dispatcher.dispatch({
			name: 'CREATE_APP_ROUTE',
			appID: this.props.appId,
			data: {
				domain: uri.hostname,
				path: uri.pathname,
				drain_backends: true
			}
		});
	}
});

export default NewAppRoute;
