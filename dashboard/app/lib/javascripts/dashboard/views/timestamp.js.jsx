var Timestamp = React.createClass({
	displayName: "Views.Timestamp",

	getInitialState: function () {
		return {
			ticks: 0
		};
	},

	componentDidMount: function () {
		this.__setRenderTimeout();
	},

	componentDidUpdate: function () {
		this.__setRenderTimeout();
	},

	componentWillUnmount: function () {
		this.__clearRenderTimeout();
	},

	render: function () {
		var timestamp = moment(this.props.timestamp);
		return (
			<span title={timestamp.format()}>
				{timestamp.fromNow()}
			</span>
		);
	},

	__setRenderTimeout: function () {
		if (this.__renderTimeout) {
			this.__clearRenderTimeout();
		}
		var timestamp = this.props.timestamp;
		var now = Date.now();
		var difference = now - timestamp;
		var timeout = null;
		if (difference > 86400000) { // more than a day ago
			timeout = 3600000; // render every hour
		} else if (difference > 3600000) { // more than an hour ago
			timeout = 1800000; // render every half hour
		} else if (difference > 60000) { // more than a minute ago
			timeout = 15000; // render every 15 seconds
		} else { // less than a minute ago
			timeout = 1000; // render every second
		}
		this.__renderTimeout = setTimeout(function () {
			this.setState({
				ticks: this.state.ticks + 1
			});
		}.bind(this), timeout);
	},

	__clearRenderTimeout: function () {
		clearTimeout(this.__renderTimeout);
		this.__renderTimeout = null;
	}
});

export default Timestamp;
