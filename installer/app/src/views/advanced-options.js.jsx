import BackupSelector from './backup-selector';
import ReleaseChannelSelector from './release-channel-selector';

var AdvancedOptions = React.createClass({
	render: function () {
		var state = this.props.state;
		return (
			<div>
				<div>
					<a href="#" onClick={this.__toggleInputs}>Advanced options</a>
				</div>
				{this.state.showInputs ? (
					<div style={{
						marginTop: 20
					}}>
						<ReleaseChannelSelector state={state} />
						<br />
						<br />
						<BackupSelector state={state} />
						<br />
						<br />
						{this.props.children}
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
	}
});

export default AdvancedOptions;
