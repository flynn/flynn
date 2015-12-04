var Step = React.createClass({
	render: function () {
		return (
			<li className={this.props.active ? "active" : (this.props.complete ? "complete" : "")}>
				<span>{this.props.label}</span>
			</li>
		);
	}
});

var InstallSteps = React.createClass({
	render: function () {
		var steps = this.state.steps.filter(function (step) {
			return step.visible !== false;
		});
		var currentStep = this.state.currentStep;
		return (
			<ul className="install-steps" style={this.props.style || {}}>
				{steps.map(function (step) {
					var active = currentStep === step.id;
					return (
						<Step
							key={step.label}
							label={step.label}
							active={active}
							complete={step.complete} />
					);
				}.bind(this))}
			</ul>
		);
	},

	getInitialState: function () {
		return this.__getState();
	},

	componentWillReceiveProps: function () {
		this.setState(this.__getState());
	},

	__getState: function () {
		var state = this.props.state;
		return {
			steps: state.steps,
			currentStep: state.currentStep,
			completedSteps: state.completedSteps
		};
	}
});
export default InstallSteps;
