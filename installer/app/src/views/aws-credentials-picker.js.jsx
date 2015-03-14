import Config from '../config';
import BtnCSS from './css/button';

var AWSCredentialsPicker = React.createClass({
	getDefaultProps: function () {
		return {
			inputCSS: {
				width: 280
			}
		};
	},

	getInitialState: function () {
		return {
			showInputs: !Config.has_aws_env_credentials
		};
	},

	render: function () {
		return (
			<div>
				<div>AWS Credentials: </div>
				<div style={{
						marginLeft: 10
					}}>
					{Config.has_aws_env_credentials && !this.state.showInputs ? (
						<div>
							Using credentials in AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.&nbsp;
							<button type="text" style={BtnCSS} onClick={function () {
									this.setState({
										showInputs: true
									});
								}.bind(this)}>Override</button>
						</div>
					) : null}
					<div style={{
							display: this.state.showInputs ? 'block' : 'none'
						}}>
						<br />
						<label>
							<div>Access Key ID: </div>
							<input
								ref="key"
								type="text"
								style={this.props.inputCSS}
								placeholder="AWS_ACCESS_KEY_ID"
								onChange={this.__handleChange} />
						</label>
						<br />
						<br />
						<label>
							<div>Secret Access Key: </div>
							<input
								ref="secret"
								type="text"
								style={this.props.inputCSS}
								placeholder="AWS_SECRET_ACCESS_KEY"
								onChange={this.__handleChange} />
						</label>
					</div>
				</div>
			</div>
		);
	},

	__handleChange: function () {
		this.props.onChange({
			access_key_id: this.refs.key.getDOMNode().value.trim(),
			secret_access_key: this.refs.secret.getDOMNode().value.trim()
		});
	}
});
export default AWSCredentialsPicker;
