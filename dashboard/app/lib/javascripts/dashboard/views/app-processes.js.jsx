import { assertEqual, extend } from 'marbles/utils';
import Modal from 'Modal';
import AppProcessesActions from '../actions/app-processes';
import IntegerPicker from './integer-picker';

var AppProcesses = React.createClass({
	displayName: "Views.AppProcesses",

	render: function () {
		var initialProcesses = this.state.initialProcesses;
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
					{this.state.showSaveConfirmModal ? (
						<section>
							<header>
								<h1>Deploy changes?</h1>
							</header>

							<ul className="app-processes-diff">
								{processNames.reduce(function (m, k) {
									return m.concat(
										initialProcesses[k] === processes[k] ? (
											<li key={k}>
												<div className="name">{k}</div>
												<div className="value">{processes[k]}</div>
											</li>
										) : ([
											<li key={k + "-old"} className="old">
												<div className="name">{k}</div>
												<div className="value">{initialProcesses[k]}</div>
											</li>,
											<li key={k + "-new"} className="new">
												<div className="name">{k}</div>
												<div className="value">{processes[k]}</div>
											</li>
										])
									);
								}, [])}
							</ul>

							<button className="btn-green" onClick={this.__handleSaveBtnConfirmClick} ref="saveConfirmBtn">Deploy</button>
						</section>
					) : null}
				</Modal>
			</section>
		);
	},

	getInitialState: function () {
		var initialProcesses = this.props.formation.processes || {};
		return {
			initialProcesses: initialProcesses,
			processes: initialProcesses,
			hasChanges: false,
			isSaving: false,
			showSaveConfirmModal: false
		};
	},

	componentWillReceiveProps: function (nextProps) {
		if ( !assertEqual(nextProps.formation, this.props.formation) ) {
			this.setState({
				processes: nextProps.formation.processes || {},
				hasChanges: false,
				isSaving: false,
				showSaveConfirmModal: false
			});
		}
	},

	__handleProcessChange: function (k, n) {
		var originalProcesses = this.props.formation.processes || {};
		var processes = extend({}, this.state.processes);
		processes[k] = n;
		this.setState({
			processes: processes,
			hasChanges: !assertEqual(originalProcesses, processes)
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
		var formation = extend({}, this.props.formation, {
			processes: this.state.processes
		});
		this.setState({
			isSaving: true,
			showSaveConfirmModal: false
		});
		AppProcessesActions.createFormation(this.props.appId, formation);
	}
});

export default AppProcesses;
