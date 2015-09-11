import AssetPaths from './asset-paths';
import Panel from './panel';
import PrettyRadio from './pretty-radio';
import Sheet from './css/sheet';

var cloudNames = {
	aws: 'AWS',
	digital_ocean: 'DigitalOcean',
	azure: 'Azure',
	ssh: 'SSH'
};

var cloudIDs = ['aws', 'digital_ocean', 'azure', 'ssh'];

var CloudSelector = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			display: 'flex',
			textAlign: 'center',
			marginBottom: '1rem',
			overflowY: 'hidden',
			overflowX: 'auto',
			selectors: [
				['> * ', {
					flexGrow: 1,
					flexBasis: '50%',

					selectors: [
						['> *', {
							padding: '20px'
						}]
					]
				}],

				['[data-img-con]', {
					display: 'table',
					margin: '0 auto',
					marginBottom: '0.25rem'
				}],

				['img', {
					display: 'table-cell',
					maxWidth: '100%',
					maxHeight: '100px',
					width: '100%',
					verticalAlign: 'middle'
				}],

				['> * + *', {
					marginLeft: '1rem'
				}]
			]
		});
		return {
			styleEl: styleEl
		};
	},

	render: function () {
		return (
			<div>
				<div id={this.state.styleEl.id}>
					{cloudIDs.map(function (cloud) {
						return (
							<Panel key={cloud} style={{padding: 0}} title={cloudNames[cloud]}>
								<PrettyRadio name='cloud' value={cloud} checked={this.props.selectedCloud === cloud} onChange={this.__handleCloudChange}>
									<div data-img-con>
										<img src={AssetPaths[cloud.replace('_', '')+'-logo.png']} alt={cloudNames[cloud]} />
									</div>
								</PrettyRadio>
							</Panel>
						);
					}.bind(this))}
				</div>
			</div>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
	},

	__handleCloudChange: function (e) {
		e.stopPropagation();
		var cloud = e.target.value;
		setTimeout(function () { // prevent invariant error
			this.props.onChange(cloud);
		}.bind(this), 0);
	}
});
export default CloudSelector;
