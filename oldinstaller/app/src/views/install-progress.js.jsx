var InstallProgress = React.createClass({
	render: function () {
		var eventNodes = [];
		var events = this.state.logEvents;
		for (var len = events.length, i = 0; i < len; i++) {
			eventNodes.push(
				<div key={i}>
					{events[i].description}
				</div>
			);
		}
		return (
				<pre ref="scrollable" style={{
					width: '100%',
					maxHeight: 500,
					overflow: 'auto'
				}}>
				{eventNodes}
			</pre>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentDidMount: function () {
		var node = this.refs.scrollable.getDOMNode();
		node.scrollTop = node.scrollHeight;
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	componentDidUpdate: function () {
		var node = this.refs.scrollable.getDOMNode();
		var __maxScrollTop = this.__maxScrollTop;
		this.__maxScrollTop = node.scrollHeight - node.clientHeight;
		if (node.scrollTop === __maxScrollTop) {
			node.scrollTop = node.scrollHeight;
		}
	},

	__getState: function () {
		return this.props.state;
	}
});
export default InstallProgress;
