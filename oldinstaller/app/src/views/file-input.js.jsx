import { extend } from 'marbles/utils';
import Colors from './css/colors';

var DragFileInput = React.createClass({
	getDefaultProps: function () {
		return {
			onChange: function(){}
		};
	},

	getInitialState: function () {
		return {
			active: false,
			file: null,
			errorMsg: null
		};
	},

	handleDragOver: function (e) {
		e.preventDefault();
		e.stopPropagation();

		this.setState({ active: true });
	},

	handleDragLeave: function (e) {
		e.preventDefault();
		e.stopPropagation();

		this.setState({ active: false });
	},

	handleDrop: function (e) {
		e.preventDefault();
		e.stopPropagation();

		this.setState({ active: false });

		var files = e.nativeEvent.target.files || e.nativeEvent.dataTransfer.files;

		if (!files || !files.length) {
			this.setState({
				file: null,
				errorMsg: "Unable to read file"
			});
			return;
		}

		if (files.length > 1) {
			this.setState({
				file: null,
				errorMsg: "You may only select one file"
			});
			return;
		}

		var file = files[0];

		if (!file || !file.size) {
			this.setState({
				errorMsg: "Invalid file"
			});
			return;
		}

		this.setState({
			file: file,
			errorMsg: null
		});
		this.props.onChange(file);
	},

	handleClick: function (e) {
		e.preventDefault();
		this.refs.input.getDOMNode().click();
	},

	handleInputChange: function (e) {
		this.handleDrop(e);
	},

	getStyles: function (state) {
		var color = Colors.grayColor;
		if (state.errorMsg !== null || this.props.errorMsg) {
			color = Colors.redColor;
		} else if (state.active) {
			color = Colors.blueColor;
		}
		return extend({
			border: '4px dashed '+ color,
			padding: '1em',

			color: Colors.grayColor,
			fontSize: '24pt',
			lineHeight: '24pt',
			textAlign: 'center'
		}, this.props.style || {});
	},

	render: function () {
		var styles = this.getStyles(this.state);
		var msg = "Select file";
		if (this.state.errorMsg !== null) {
			msg = this.state.errorMsg;
		} else if (this.state.file !== null) {
			msg = this.state.file.name;
		}
		if (this.props.errorMsg) {
			msg = this.props.errorMsg;
		}
		return (
			<div>
				<div style={styles} onDragOver={this.handleDragOver} onDragLeave={this.handleDragLeave} onDrop={this.handleDrop} onClick={this.handleClick}>
					{msg}
				</div>
				<input ref="input" type="file" style={{ display: 'none' }} onChange={this.handleInputChange} />
			</div>
		);
	}
});

export default DragFileInput;
