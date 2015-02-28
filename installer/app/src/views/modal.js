var Modal = React.createClass({
	displayName: "Modal",

	getInitialState: function () {
		return {
			visible: false
		};
	},

	getDefaultProps: function () {
		return {
			onShow: function(){},
			onHide: function(){},
			closable: true
		};
	},

	componentWillMount: function () {
		this.handleBeforeComponentUpdate(this.props);
	},

	componentWillUnmount: function () {
		this.__setBodyOverflowVisible(true);
	},

	componentWillReceiveProps: function (props) {
		this.handleBeforeComponentUpdate(props);
	},

	componentDidMount: function () {
		this.handleComponentUpdate(this.props, this.getInitialState());
	},

	componentDidUpdate: function (prevProps, prevState) {
		this.handleComponentUpdate(prevProps, prevState);
	},

	handleBeforeComponentUpdate: function (props) {
		if (props.hasOwnProperty('visible') && props.visible !== this.state.visible) {
			this.setState({
				visible: props.visible
			});
		}
	},

	handleComponentUpdate: function (prevProps, prevState) {
		if (prevState.visible !== this.state.visible) {
			if (this.state.visible) {
				this.props.onShow();
			} else {
				this.props.onHide();
			}
			this.__setBodyOverflowVisible(!this.state.visible);
		}
	},

	__setBodyOverflowVisible: function (visible) {
		if (!visible) {
			document.body.style.overflow = 'hidden';
		} else {
			document.body.style.overflow = null;
		}
	},

	handleCloseBtnClick: function (e) {
		e.preventDefault();
		e.stopPropagation();
		this.toggleVisibility();
	},

	handleOverlayClick: function (e) {
		if (e.target === this.refs.overlay.getDOMNode() && this.props.closable) {
			e.preventDefault();
			e.stopPropagation();
			this.toggleVisibility();
		}
	},

	// called from the outside world
	toggleVisibility: function () {
		var visible = !this.state.visible;
		this.setState({
			visible: visible
		});
	},

	// called from the outside world
	show: function () {
		this.setState({ visible: true });
	},

	// called from the outside world
	hide: function () {
		this.setState({ visible: false });
	},

	render: function () {
		var closeBtn = React.createElement('div', { className: "overlay-top" }, React.createElement('div', {
			className: "overlay-close",
			title: "Close",
			onClick: this.handleCloseBtnClick
		}, "×"));

		if (this.props.closable === false) {
			closeBtn = React.createElement('div');
		}

		return (
			React.createElement('div', {
				className: "overlay"+ (this.state.visible ? "" : " hidden") + (this.props.className ? " "+ this.props.className : ""),
				ref: "overlay",
				onClick: this.handleOverlayClick
			}, closeBtn,

				React.createElement('div', { className: "overlay-content" }, this.props.children)));
	}
});

export default Modal;
