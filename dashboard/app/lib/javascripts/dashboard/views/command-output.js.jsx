var padding = function (str, len) {
	if (str.length === len) {
		return str;
	}
	for (var i = str.length; i < len; i++) {
		str += " ";
	}
	return str;
};

var CommandOutput = React.createClass({
	displayName: "Views.CommandOutput",

	getDefaultProps: function () {
		return {
			showTimestamp: true
		};
	},

	render: function () {
		var data = this.__formatOutputStream(this.props.outputStreamData);
		return (
			<pre ref="pre" className="command-output">
				{data}
			</pre>
		);
	},

	componentDidUpdate: function () {
		var pre = this.refs.pre.getDOMNode();
		var __maxScrollTop = this.__maxScrollTop;
		this.__maxScrollTop = pre.scrollHeight - pre.clientHeight;
		if (pre.scrollTop === __maxScrollTop) {
			pre.scrollTop = pre.scrollHeight;
		}
	},

	__formatOutputStream: function (outputStreamData) {
		var showTimestamp = this.props.showTimestamp;
		var data = outputStreamData.map(function (item) {
			var timestamp = "";
			if (showTimestamp) {
				timestamp = padding("["+ item.timestamp +"] ", 33);
			}
			return timestamp + item.msg;
		}).join("\n");
		data = data.replace(/\r\r/g, '\r')
			.replace(/\033\[K\r/g, '\r')
			.replace(/\[2K/g, '')
			.replace(/\033\(B/g, '')
			.replace(/\033\[\d+G/g, '');
		data = window.ansiparse(data).map(function (item) { return item.text; }).join("\n");
		return data;
	}

});

export default CommandOutput;
