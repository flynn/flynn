import AssetPaths from './asset-paths';
import Panel from './panel';
import PrettyRadio from './pretty-radio';
import Sheet from './css/sheet';

var cloudNames = {
	aws: 'AWS',
	digital_ocean: 'DigitalOcean'
};

var cloudIDs = ['aws', 'digital_ocean'];

var CloudSelector = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			display: 'flex',
			textAlign: 'center',
			marginBottom: '1rem',
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

				['img', {
					height: '100px'
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
		var state = this.props.state;
		return (
			<div>
				<div id={this.state.styleEl.id}>
					{cloudIDs.map(function (cloud) {
						return (
							<Panel key={cloud} style={{padding: 0}}>
								<PrettyRadio name='cloud' value={cloud} checked={state.selectedCloud === cloud} onChange={this.__handleCloudChange}>
									<img src={AssetPaths[cloud.replace('_', '')+'-logo.png']} alt={cloudNames[cloud]} />
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
		var cloud = e.target.value;
		this.props.onChange(cloud);
	}
});
export default CloudSelector;
