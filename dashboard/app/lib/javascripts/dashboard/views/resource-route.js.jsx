var ResourceRoute = React.createClass({
	displayName: "Views.ResourceRoute",

	render: function () {
		var props = this.props;
		var state = this.state;
		var cmdName = props.cmds.name;
		var cmd = state.showExternal ? props.cmds.external : props.cmds.internal;
		var url = state.showExternal ? props.urls.external : props.urls.internal;
		return (
			<div className="resource-route">
				<div onClick={this.__handleInternalExternalToggleClick}>
					{state.showExternal ? 'External' : 'Internal'}
				</div>
				<div onClick={this.__handleCmdURLToggleClick}>
					{state.showCmd ? cmdName : 'URL'}
				</div>
				<input
					type="text" 
					value={state.showCmd ? cmd : url}
					onChange={function(){}}
					onClick={function (e) {
						e.target.setSelectionRange(0, e.target.value.length);
					}} />
			</div>
		);
	},

	getInitialState: function () {
		var props = this.props;
		return {
			showExternal: props.hasExternal ? true : false,
			showCmd: true
		};
	},

	componentWillReceiveProps: function (nextProps) {
		if ( !this.props.hasExternal && nextProps.hasExternal ) {
			this.setState({
				showExternal: true
			});
		}
	},

	__handleInternalExternalToggleClick: function () {
		if ( !this.props.hasExternal ) {
			return;
		}
		this.setState({
			showExternal: !this.state.showExternal
		});
	},

	__handleCmdURLToggleClick: function () {
		this.setState({
			showCmd: !this.state.showCmd
		});
	}
});

export default ResourceRoute;
