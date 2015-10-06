import Modal from 'Modal';
import Dispatcher from 'dashboard/dispatcher';
import AppRouteDeleteStore from 'dashboard/stores/app-route-delete';

var AppRouteDelete = React.createClass({
	displayName: "Views.AppRouteDelete",

	render: function () {
		var domain = this.props.domain;
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-delete">
					<header>
						<h1>Delete route{domain ? " ("+ domain +")": ""}?</h1>
					</header>

					{this.state.errorMsg ? (
						<div className="alert-error">{this.state.errorMsg}</div>
					) : null}

					<button className="delete-btn" disabled={this.state.isDeleting} onClick={this.__handleDeleteBtnClick}>{this.state.isDeleting ? "Please wait..." : "Delete"}</button>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return this.__getState(this.props);
	},

	componentDidMount: function () {
		AppRouteDeleteStore.addChangeListener(this.__getStoreID(this.props), this.__handleStoreChange);
	},

	componentWillUnmount: function () {
		AppRouteDeleteStore.removeChangeListener(this.__getStoreID(this.props), this.__handleStoreChange);
	},

	__handleDeleteBtnClick: function (e) {
		e.preventDefault();
		Dispatcher.dispatch({
			name: 'DELETE_APP_ROUTE',
			appID: this.props.appId,
			routeType: this.props.routeType,
			routeID: this.props.routeId
		});
	},

	__handleStoreChange: function () {
		if (this.isMounted()) {
			this.setState(this.__getState(this.props));
		}
	},

	__getStoreID: function (props) {
		return {
			appID: props.appId,
			routeID: props.routeId
		};
	},

	__getState: function (props) {
		return AppRouteDeleteStore.getState(this.__getStoreID(props));
	}
});

export default AppRouteDelete;
