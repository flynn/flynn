/** @jsx React.DOM */
//= require ../actions/app-processes
//= require ./integer-picker
//= require Modal

(function () {

"use strict";

var AppProcessesActions = FlynnDashboard.Actions.AppProcesses;

var IntegerPicker = FlynnDashboard.Views.IntegerPicker;
var Modal = window.Modal;

FlynnDashboard.Views.AppProcesses = React.createClass({
	displayName: "Views.AppProcesses",

	render: function () {
		var processes = this.state.processes;
		var processNames = Object.keys(processes).sort();

		return (
			<section className="app-processes">
				<header>
					<h2>Processes</h2>
				</header>

				<ul>
					{processNames.map(function (k) {
						return (
							<li key={k}>
								<div className="name">{k}</div>
								<IntegerPicker
									value={processes[k]}
									onChange={this.__handleProcessChange.bind(this, k)} />
							</li>
						);
					}, this)}

					<li className="save-btn-container">
						<button
							className="btn-green"
							disabled={ !this.state.hasChanges || this.state.isSaving }
							onClick={this.__handleSaveBtnClick}>{this.state.isSaving ? "Please wait..." : "Save"}</button>
					</li>
				</ul>

				<Modal visible={this.state.showSaveConfirmModal} onShow={this.__handleSaveConfirmModalShow} onHide={this.__handleSaveConfirmModalHide}>
					<section>
						<header>
							<h1>Deploy changes?</h1>
						</header>

						<button className="btn-green" onClick={this.__handleSaveBtnConfirmClick} ref="saveConfirmBtn">Deploy</button>
					</section>
				</Modal>
			</section>
		);
	},

	getInitialState: function () {
		return {
			processes: this.props.formation.processes, // initial value
			hasChanges: false,
			isSaving: false,
			showSaveConfirmModal: false
		};
	},

	componentWillReceiveProps: function (nextProps) {
		if ( !Marbles.Utils.assertEqual(nextProps.formation, this.props.formation) ) {
			this.setState({
				processes: nextProps.formation.processes,
				hasChanges: false,
				isSaving: false,
				showSaveConfirmModal: false
			});
		}
	},

	__handleProcessChange: function (k, n) {
		var originalProcesses = this.props.formation.processes;
		var processes = Marbles.Utils.extend({}, this.state.processes);
		processes[k] = n;
		this.setState({
			processes: processes,
			hasChanges: !Marbles.Utils.assertEqual(originalProcesses, processes)
		});
	},

	__handleSaveBtnClick: function (e) {
		e.preventDefault();
		this.setState({
			showSaveConfirmModal: true
		});
	},

	__handleSaveConfirmModalShow: function () {
		this.refs.saveConfirmBtn.getDOMNode().focus();
	},

	__handleSaveConfirmModalHide: function () {
		this.setState({
			showSaveConfirmModal: false
		});
	},

	__handleSaveBtnConfirmClick: function (e) {
		e.preventDefault();
		var formation = Marbles.Utils.extend({}, this.props.formation, {
			processes: this.state.processes
		});
		this.setState({
			isSaving: true,
			showSaveConfirmModal: false
		});
		AppProcessesActions.createFormation(formation);
	}
});

})();
