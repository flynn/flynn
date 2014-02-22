/** @jsx React.DOM */

Flynn.Views.Modal = React.createClass({
	displayName: "Flynn.Views.Modal",

	getInitialState: function () {
		return {
			visible: false
		};
	},

	componentDidMount: function () {
		this.handleComponentUpdate();
	},

	componentDidUpdate: function () {
		this.handleComponentUpdate();
	},

	handleComponentUpdate: function () {
		if (this.state.visible) {
			document.body.style.overflow = 'hidden';
		} else {
			document.body.style.overflow = null;
		}
	},

	handleCloseBtnClick: function (e) {
		e.preventDefault();
		this.toggleVisibility();
	},

	handleOverlayClick: function (e) {
		if (e.target === this.refs.overlay.getDOMNode()) {
			this.toggleVisibility();
		}
	},

	// called from the outside world
	toggleVisibility: function () {
		this.setState({
			visible: !this.state.visible
		});
	},

	render: function () {
		return (
			<div
				className={"overlay"+ (this.state.visible ? "" : " hidden")}
				ref="overlay"
				onClick={this.handleOverlayClick}>

				<div className="overlay-top">
					<div
						className="overlay-close"
						title="Close sponsor form"
						onClick={this.handleCloseBtnClick}>&times;</div>
				</div>

				<div className="overlay-content">
					{this.props.children}
				</div>
			</div>
		);
	}
});
