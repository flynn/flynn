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
		var steps = this.state.steps;
		var currentStep = this.state.currentStep;
		var completedSteps = this.state.completedSteps;
		return (
			<ul className="install-steps">
				{steps.map(function (step) {
					var complete = completedSteps.indexOf(step.id) !== -1;
					var active = currentStep === step.id;
					return (
						<Step
							key={step.label}
							label={step.label}
							active={active}
							complete={complete} />
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
