import PrettySelect from './pretty-select';

var AWSInstanceTypePicker = React.createClass({
	render: function () {
		return (
			<label>
				<div>AWS Instance Type: </div>
				<PrettySelect onChange={this.__handleChange} value={this.props.value}>
					<optgroup label="General purpose">
						<option value="t2.medium">t2.medium</option>
						<option value="m3.medium">m3.medium</option>
						<option value="m3.large">m3.large</option>
						<option value="m3.xlarge">m3.xlarge</option>
						<option value="m3.2xlarge">m3.2xlarge</option>
					</optgroup>

					<optgroup label="Compute optimized">
						<option value="c4.large">c4.large</option>
						<option value="c4.xlarge">c4.xlarge</option>
						<option value="c4.2xlarge">c4.2xlarge</option>
						<option value="c4.4xlarge">c4.4xlarge</option>
						<option value="c4.8xlarge">c4.8xlarge</option>
						<option value="c3.large">c3.large</option>
						<option value="c3.xlarge">c3.xlarge</option>
						<option value="c3.2xlarge">c3.2xlarge</option>
						<option value="c3.4xlarge">c3.4xlarge</option>
						<option value="c3.8xlarge">c3.8xlarge</option>
					</optgroup>

					<optgroup label="Memory optimized">
						<option value="r3.large">r3.large</option>
						<option value="r3.xlarge">r3.xlarge</option>
						<option value="r3.2xlarge">r3.2xlarge</option>
						<option value="r3.4xlarge">r3.4xlarge</option>
						<option value="r3.8xlarge">r3.8xlarge</option>
					</optgroup>

					<optgroup label="Storage optimized">
						<option value="i2.xlarge">i2.xlarge</option>
						<option value="i2.2xlarge">i2.2xlarge</option>
						<option value="i2.4xlarge">i2.4xlarge</option>
						<option value="i2.8xlarge">i2.8xlarge</option>
						<option value="hs1.8xlarge">hs1.8xlarge</option>
					</optgroup>

					<optgroup label="GPU instances">
						<option value="g2.2xlarge">g2.2xlarge</option>
					</optgroup>
				</PrettySelect>
			</label>
		);
	},

	__handleChange: function (e) {
		var instanceType = e.target.value;
		this.props.onChange(instanceType);
	}
});
export default AWSInstanceTypePicker;
