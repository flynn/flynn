//= require ../stores/app
//= require ../actions/app-env
//= require ./edit-env
//= require Modal

(function () {

"use strict";

var AppStore = Dashboard.Stores.App;

var AppEnvActions = Dashboard.Actions.AppEnv;

var Modal = window.Modal;

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
	state.release = appState.release;

	if (state.release && ( !prevState.release || !Marbles.Utils.assertEqual(prevState.release, state.release) )) {
		state.env = Marbles.Utils.extend({}, state.release.env);
		state.hasChanges = false;
		state.isSaving = false;
	}

	return state;
}

Dashboard.Views.AppEnv = React.createClass({
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
							<Dashboard.Views.EditEnv env={env} onChange={this.__handleEnvChange} />
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
		if ( !Marbles.Utils.assertEqual(prevAppStoreId, nextAppStoreId) ) {
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
		this.setState({
			env: env,
			hasChanges: !Marbles.Utils.assertEqual(env, this.state.release.env)
		});
	},

	__handleSaveBtnClick: function (e) {
		e.preventDefault();
		var release = Marbles.Utils.extend({}, this.state.release, {
			env: this.state.env
		});
		delete release.id;
		this.setState({
			isSaving: true
		});
		AppEnvActions.createRelease(this.state.appStoreId, release);
	}
});

})();
