import Panel from './panel';

var InstallProgress = React.createClass({
	render: function () {
		var eventNodes = [];
		var events = this.state.installEvents;
		for (var len = events.length, i = 0; i < len; i++) {
			eventNodes.push(
				<div key={i}>
					{events[i].description}
				</div>
			);
		}
		return (
			<Panel>
					<pre ref="scrollable" style={{
						width: '100%',
						maxHeight: 500,
						overflow: 'auto'
					}}>
					{eventNodes}
				</pre>
			</Panel>
		);
	},

	getInitialState: function () {
		return this.__getState();
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
