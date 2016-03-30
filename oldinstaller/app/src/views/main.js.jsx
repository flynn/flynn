import Sheet from './css/sheet';
import Panel from './panel';
import Clusters from './clusters';
import Prompt from './prompt';
import ProgressMeter from './progress-meter';
import BtnCSS from './css/button';

var Main = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			margin: '16px',
			display: 'flex',
			selectors: [
				['> *', {
					flexGrow: 1
				}]
			]
		});
		var credsBtnStyleEl = Sheet.createElement(BtnCSS, {
			position: 'absolute',
			bottom: '20px',
			right: '20px'
		});
		return {
			styleEl: styleEl,
			credsBtnStyleEl: credsBtnStyleEl
		};
	},

	render: function () {
		var state = this.props.dataStore.state;
		var prompt = state.prompts[state.currentClusterID] || null;
		var progressMeters = state.progressMeters[state.currentClusterID] || {};
		return (
			<div id={this.state.styleEl.id}>
				<div style={{
					marginRight: 16,
					maxWidth: 360,
					minWidth: 300,
					flexBasis: 360
				}}>
					<Panel style={{ height: '100%', position: 'relative', paddingBottom: 80 }}>
						<Clusters state={state} />
					</Panel>
				</div>

				<div style={{
					width: 'calc(100% - 360px)',
					height: '100%'
				}}>
					{prompt ? (
						<Panel style={{ marginBottom: '1rem' }}>
							<Prompt
								key={prompt.id+state.currentClusterID}
								prompt={prompt}
								state={state} />
						</Panel>
					) : null}

					{state.currentCluster.attrs.state === 'starting' ? Object.keys(progressMeters).sort().map(function (mID) {
						var m = progressMeters[mID];
						return (
							<Panel key={mID} style={{ marginBottom: '1rem' }}>
								<ProgressMeter percent={m.percent} description={m.description} />
							</Panel>
						);
					}) : null}

					{this.props.children}
				</div>
			</div>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
		this.state.credsBtnStyleEl.commit();
		this.props.dataStore.addChangeListener(this.__handleDataChange);
	},

	componentWillUnmount: function () {
		this.props.dataStore.removeChangeListener(this.__handleDataChange);
	},

	__handleDataChange: function () {
		this.forceUpdate();
	}
});
export default Main;
