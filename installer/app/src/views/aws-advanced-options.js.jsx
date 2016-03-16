import AdvancedOptions from './advanced-options';

var AWSAdvancedOptions = React.createClass({
	render: function () {
		var state = this.props.state;
		return (
			<AdvancedOptions state={state}>
				<label>
					<div>CIDR block to assign to the VPC: </div>
					<input
						type="text"
						placeholder="10.0.0.0/16"
						value={state.vpcCidr}
						onChange={this.__handleVpcCidrChange} />
				</label>
				<br />
				<br />
				<label>
					<div>CIDR block to assign to the subnet: </div>
					<input
						type="text"
						placeholder="10.0.0.0/21"
						value={state.subnetCidr}
						onChange={this.__handleSubnetCidrChange} />
				</label>
			</AdvancedOptions>
		);
	},

	__handleVpcCidrChange: function (e) {
		this.__vpcCidr = e.target.value.trim();
		this.__triggerOnChange();
	},

	__handleSubnetCidrChange: function (e) {
		this.__subnetCidr = e.target.value.trim();
		this.__triggerOnChange();
	},

	__triggerOnChange: function () {
		var values = {};
		if (this.__vpcCidr) {
			values.vpcCidr = this.__vpcCidr;
		}
		if (this.__subnetCidr) {
			values.subnetCidr = this.__subnetCidr;
		}
		this.props.onChange(values);
	}
});

export default AWSAdvancedOptions;
