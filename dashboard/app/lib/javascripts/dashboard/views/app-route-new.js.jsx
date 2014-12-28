//= require ../actions/app-route-new
//= require Modal

(function () {

"use strict";

var NewAppRouteActions = Dashboard.Actions.NewAppRoute;

var Modal = window.Modal;

Dashboard.Views.NewAppRoute = React.createClass({
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

						{this.props.errorMsg ? (
							<div className="alert-error">{this.props.errorMsg}</div>
						) : null}

						<label>
							<div className="name">Domain</div>
							<input type="text" ref="domain" value={this.state.domain} onChange={this.__handleDomainChange} />
						</label>

						<button type="submit" className="create-btn" disabled={ !this.state.domain || this.state.isCreating }>{this.state.isCreating ? "Please wait..." : "Add Domain"}</button>
					</form>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return {
			isCreating: false,
			domain: null
		};
	},

	componentDidMount: function () {
		this.refs.domain.getDOMNode().focus();
	},

	componentWillReceiveProps: function (nextProps) {
		if (nextProps.errorMsg) {
			this.setState({
				isCreating: false
			});
		}
	},

	__handleDomainChange: function (e) {
		var domain = e.target.value.trim();
		this.setState({
			domain: domain
		});
	},

	__handleFormSubmit: function (e) {
		e.preventDefault();
		this.setState({
			isCreating: true
		});
		NewAppRouteActions.createAppRoute(this.props.appId, this.state.domain);
	}
});

})();
