import Modal from 'Modal';
import AppRouteDeleteActions from '../actions/app-route-delete';

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

					{this.props.errorMsg ? (
						<div className="alert-error">{this.props.errorMsg}</div>
					) : null}

					<button className="delete-btn" disabled={this.state.isDeleting} onClick={this.__handleDeleteBtnClick}>{this.state.isDeleting ? "Please wait..." : "Delete"}</button>
				</section>
			</Modal>
		);
	},

	getInitialState: function () {
		return {
			isDeleting: false
		};
	},

	componentWillReceiveProps: function (nextProps) {
		if (nextProps.errorMsg) {
			this.setState({
				isDeleting: false
			});
		}
	},

	__handleDeleteBtnClick: function (e) {
		e.preventDefault();
		this.setState({
			isDeleting: true
		});
		AppRouteDeleteActions.deleteAppRoute(this.props.appId, this.props.routeType, this.props.routeId);
	}
});

export default AppRouteDelete;
