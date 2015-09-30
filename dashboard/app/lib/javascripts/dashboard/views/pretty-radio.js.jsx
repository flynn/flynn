var PrettyRadio = React.createClass({
	render: function () {
		return (
			<label className={"pretty-radio"}>
				<input type="radio" name="selected-sha" checked={this.props.checked} onChange={this.props.onChange} />
				<div className={"dot"} />

				{this.props.children}
			</label>
		);
	}
});

export default PrettyRadio;
