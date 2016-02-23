var PrettyRadio = React.createClass({
	render: function () {
		var input = [
			<input key={1} className={this.props.hideDot ? 'hidden': ''} type="radio" name="selected-sha" checked={this.props.checked} onChange={this.props.onChange} />,
			<div key={2} className={"dot"} />
		];
		if (this.props.hideDot) {
			input.pop();
		}
		return (
			<label className={"pretty-radio"} style={this.props.style}>
				{this.props.dotAfter ? null : input}

				{this.props.children}

				{this.props.dotAfter ? input : null}
			</label>
		);
	}
});

export default PrettyRadio;
