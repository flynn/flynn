import { assertEqual } from 'marbles/utils';
import Modal from 'Modal';
import AppStore from '../stores/app';
import Dispatcher from 'dashboard/dispatcher';

function getAppStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props, prevState) {
	prevState = prevState || {};
	var state = {
		appStoreId: getAppStoreId(props),
		env: prevState.env
	};

	var appState = AppStore.getState(state.appStoreId);
	state.app = appState.app;
	state.notFound = appState.notFound;
	state.error = null;

	if (state.notFound) {
		state.error = "App not found";
	} else if (appState.deleteError) {
		state.error = appState.deleteError;
	}

	if (state.error) {
		state.isDeleting = false;
	}

	return state;
}

var AppDelete = React.createClass({
	displayName: "Views.AppDelete",

	render: function () {
		var app = this.state.app;
		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-delete">
					<header>
						<h1>Delete {app ? app.name : "app"}?</h1>
					</header>

					{this.state.error ? (
						<p className="alert-error">{this.state.error}</p>
					) : null}

					<button className="delete-btn" disabled={ !app || this.state.isDeleting } onClick={this.__handleDeleteBtnClick}>{this.state.isDeleting ? "Please wait..." : "Delete"}</button>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return getState(this.props);
	},

	componentDidMount: function () {
		AppStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppStoreId = this.state.appStoreId;
		var nextAppStoreId = getAppStoreId(nextProps);
		if ( !assertEqual(prevAppStoreId, nextAppStoreId) ) {
			AppStore.removeChangeListener(prevAppStoreId, this.__handleStoreChange);
			AppStore.addChangeListener(nextAppStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppStore.removeChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	__handleStoreChange: function (props) {
		this.setState(getState(props || this.props, this.state));
	},

	__handleDeleteBtnClick: function (e) {
		e.preventDefault();
		this.setState({
			isDeleting: true
		});
		Dispatcher.dispatch({
			name: 'DELETE_APP',
			appID: this.props.appId
		});
	}
});

export default AppDelete;
