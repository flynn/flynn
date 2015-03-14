import PrettySelect from './pretty-select';

var AWSRegionPicker = React.createClass({
	render: function () {
		return (
			<label>
				<div>AWS Region: </div>
				<PrettySelect onChange={this.__handleChange} value={this.props.value}>
					<option value="us-east-1">US East (N. Virginia)</option>
					<option value="us-west-2">US West (Oregon)</option>
					<option value="us-west-1">US West (N. California)</option>
					<option value="eu-west-1">EU (Ireland)</option>
					<option value="eu-central-1">EU (Frankfurt)</option>
					<option value="ap-southeast-1">Asia Pacific (Singapore)</option>
					<option value="ap-southeast-2">Asia Pacific (Sydney)</option>
					<option value="ap-northeast-1">Asia Pacific (Tokyo)</option>
					<option value="sa-east-1">South America (Sao Paulo)</option>
				</PrettySelect>
			</label>
		);
	},

	__handleChange: function (e) {
		var region = e.target.value;
		this.props.onChange(region);
	}
});
export default AWSRegionPicker;
