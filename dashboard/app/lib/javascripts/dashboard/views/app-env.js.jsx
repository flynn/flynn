import { assertEqual, extend } from 'marbles/utils';
import { objectDiff, applyObjectDiff } from 'dashboard/utils';
import Modal from 'Modal';
import AppStore from '../stores/app';
import Dispatcher from 'dashboard/dispatcher';
import EditEnv from './edit-env';

function getAppStoreId (props) {
	return {
		appId: props.appId
	};
}

function getState (props, prevState) {
	prevState = prevState || {};
	var state = {
		appStoreId: getAppStoreId(props),
		env: prevState.env,
		envDiff: prevState.envDiff || []
	};

	var appState = AppStore.getState(state.appStoreId);
	state.app = appState.app;
	state.release = appState.release;

	if (state.release && !assertEqual(prevState.release, state.release)) {
		state.env = applyObjectDiff(state.envDiff, extend({}, state.release.env || {}));
		state.hasChanges = !assertEqual(state.release.env || {}, state.env);
		state.envDiff = [];
		state.isSaving = false;
	}

	return state;
}

var AppEnv = React.createClass({
	displayName: "Views.AppEnv",

	render: function () {
		var env = this.state.env;

		return (
			<Modal onShow={function(){}} onHide={this.props.onHide} visible={true}>
				<section className="app-env">
					<header>
						<h1>App environment</h1>
					</header>
					{env ? (
						<div>
							<EditEnv env={env} onChange={this.__handleEnvChange} onSubmit={this.__handleSubmit} disabled={this.state.isSaving} />
							<button className="save-btn" onClick={this.__handleSaveBtnClick} disabled={ !this.state.hasChanges || this.state.isSaving }>{this.state.isSaving ? "Please wait..." : "Save"}</button>
						</div>
					) : null}
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

	__handleEnvChange: function (env) {
		var diff = objectDiff(this.state.release.env || {}, env);
		this.setState({
			env: env,
			envDiff: diff,
			hasChanges: diff.length > 0
		});
	},

	__handleSubmit: function () {
		this.setState({
			isSaving: true
		});
		Dispatcher.dispatch({
			name: 'UPDATE_APP_ENV',
			appID: this.props.appId,
			prevRelease: this.state.release,
			data: this.state.env,
			deployTimeout: this.state.app.deploy_timeout
		});
	},

	__handleSaveBtnClick: function (e) {
		e.preventDefault();
		this.__handleSubmit();
	}
});

export default AppEnv;
