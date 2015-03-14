var AWSAdvancedOptions = React.createClass({
	render: function () {
		return (
			<div>
				<div>
					<a href="#" onClick={this.__toggleInputs}>Advanced options</a>
				</div>
				{this.state.showInputs ? (
					<div style={{
						marginTop: 20
					}}>
						<label>
							<div>CIDR block to assign to the VPC: </div>
							<input
								type="text"
								placeholder="10.0.0.0/16"
								onChange={this.__handleVpcCidrChange} />
						</label>
						<br />
						<br />
						<label>
							<div>CIDR block to assign to the subnet: </div>
							<input
								type="text"
								placeholder="10.0.0.0/21"
								onChange={this.__handleSubnetCidrChange} />
						</label>
					</div>
				) : null}
			</div>
		);
	},

	getInitialState: function () {
		return {
			showInputs: false
		};
	},

	__toggleInputs: function (e) {
		e.preventDefault();
		this.setState({
			showInputs: !this.state.showInputs
		});
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
