/** @jsx React.DOM */
//= require ./input
//= require ./models/login

(function () {

"use strict";

var Input = FlynnDashboard.Views.Input;
var LoginModel = FlynnDashboard.Views.Models.Login;

FlynnDashboard.Views.Login = React.createClass({
	displayName: "Views.Login",

	componentDidMount: function () {
		LoginModel.addChangeListener(this.__handleLoginModelChange);
		this.refs.token.getDOMNode().focus();
	},

	componentWillUnmount: function () {
		LoginModel.removeChangeListener(this.__handleLoginModelChange);
	},

	render: function () {
		return (
			<section>
				<header>
					<h1>Log in</h1>
				</header>

				<form className="login-form" noValidate={true} onSubmit={this.__handleSubmit}>
					<Input ref="token" type="password" name="token" label="Login token" valueLink={LoginModel.getValueLink("token")} />

					<button type="submit" disabled={this.__isSubmitDisabled()}>Login</button>
				</form>
			</section>
		);
	},

	__isSubmitDisabled: function () {
		return !LoginModel.isValid() && !LoginModel.isPersisting();
	},

	__handleLoginModelChange: function () {
		this.forceUpdate();
	},

	__handleSubmit: function (e) {
		e.preventDefault();
		this.refs.token.setChanging(false);
		LoginModel.performLogin().then(this.props.onSuccess, function(){});
	}
});

})();
