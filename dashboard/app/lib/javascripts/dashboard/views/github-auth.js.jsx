//= require ../stores/app
//= require ../actions/app-auth
//= require ./route-link
//= require ./external-link

(function () {

"use strict";

var AppStore = Dashboard.Stores.App;

var AppAuthActions = Dashboard.Actions.AppAuth;

var RouteLink = Dashboard.Views.RouteLink;
var ExternalLink = Dashboard.Views.ExternalLink;

Dashboard.Views.GithubAuth = React.createClass({
	displayName: "Views.GithubAuth",

	render: function () {
		return (
			<section className="github-auth-container">
				<header>
					<h1>Connect with GitHub</h1>
					<RouteLink path="/" className="back-link">
						Go back to cluster
					</RouteLink>
				</header>

				<section className="panel github-auth">
					<form onSubmit={this.__handleSubmit}>
						{this.state.notFound || this.props.errorMsg ? (
							<div className="alert-error">
								{this.state.notFound ? "Error: App not found: "+ this.props.appName : this.props.errorMsg}
							</div>
						) : null}

						<p>If you’d like to deploy GitHub repos with Flynn, you’ll need to generate an application token for your account. This access is limited, read-only, and can be revoked at any time on the <ExternalLink href="https://github.com/settings/applications">GitHub control panel</ExternalLink>.</p>

						<ol>
							<li>
								<ExternalLink href={"https://github.com/settings/tokens/new"+ Marbles.QueryParams.serializeParams([{
										scopes: "repo,read:org,read:public_key",
										description: "Flynn Dashboard"
									}])} className="btn-green connect-with-github" onClick={this.__handleGenerateTokenBtnClick}>
									<i className="icn-github-mark" />
									Generate Token
								</ExternalLink>

								<p>Click the button above to request the token from GitHub. The name and permissions should already be completed for you, just like the screen shot below.</p>

								<div>
									<img id="github-token-gen" src={Dashboard.config.ASSET_PATHS['github-token-gen.png']} alt="Generate Token" />
								</div>
							</li>

							<li>
								<p>Once you create a key, be sure to copy it into the field below. Once you’ve pasted the key, click the Save button to add the key to Flynn.</p>

								<label>
									<span className="text">Token</span>
									<input type="text" ref="githubToken" onChange={this.__handleGithubTokenChange} />
								</label>

								<button type="submit" disabled={this.state.isSaving || this.state.submitDisabled || !this.state.release} className="btn-green">
									{this.state.isSaving ? "Please wait..." : "Save"}
								</button>

								<div>
									<img id="github-token-copy" src={Dashboard.config.ASSET_PATHS['github-token-copy.png']} alt="Copy Token" />
								</div>
							</li>
						</ol>
					</form>
				</section>
			</section>
		);
	},

	getInitialState: function () {
		return Marbles.Utils.extend(this.__getState(this.props), {
			githubToken: "",
			submitDisabled: true
		});
	},

	componentDidMount: function () {
		AppStore.addChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	componentWillReceiveProps: function (nextProps) {
		var prevAppStoreId = this.state.appStoreId;
		var nextAppStoreId = this.__getAppStoreId(nextProps);
		if ( !Marbles.Utils.assertEqual(prevAppStoreId, nextAppStoreId) ) {
			AppStore.removeChangeListener(prevAppStoreId, this.__handleStoreChange);
			AppStore.addChangeListener(nextAppStoreId, this.__handleStoreChange);
			this.__handleStoreChange(nextProps);
		}
	},

	componentWillUnmount: function () {
		AppStore.removeChangeListener(this.state.appStoreId, this.__handleStoreChange);
	},

	__handleGenerateTokenBtnClick: function () {
		this.refs.githubToken.getDOMNode().focus();
	},

	__handleGithubTokenChange: function () {
		var token = this.refs.githubToken.getDOMNode().value.trim();
		this.setState({
			githubToken: token,
			submitDisabled: token === ""
		});
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		var release = Marbles.Utils.extend({}, this.state.release, {
			env: Marbles.Utils.extend({}, this.state.release.env, {
				GITHUB_TOKEN: this.state.githubToken
			})
		});
		delete release.id;
		this.setState({
			isSaving: true
		});
		AppAuthActions.setGithubToken(this.state.githubToken);
		AppAuthActions.createRelease(this.state.appStoreId, release);
	},

	__handleStoreChange: function (props) {
		this.setState(this.__getState(props || this.props, this.state));
	},

	__getAppStoreId: function (props) {
		return {
			appId: props.appName
		};
	},

	__getState: function (props, prevState) {
		prevState = prevState || {};
		var state = {
			appStoreId: this.__getAppStoreId(props),
			githubToken: prevState.githubToken,
			submitDisabled: prevState.submitDisabled
		};

		var appState = AppStore.getState(state.appStoreId);
		state.notFound = appState.notFound;
		state.app = appState.app;
		state.release = appState.release;
		if (state.release) {
			state.env = Marbles.Utils.extend({}, state.release.env);
		}

		return state;
	}

});

})();
