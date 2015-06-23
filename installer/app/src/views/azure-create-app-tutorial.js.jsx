import AssetPaths from './asset-paths';
import ExternalLink from './external-link';
import Colors from './css/colors';
import Sheet from './css/sheet';
import InputSelection from './input-selection';

var AzureCreateAppTutorial = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			marginTop: '1rem',
			selectors: [
				['> li > img', {
					verticalAlign: 'text-top',
					border: '1px solid '+ Colors.almostBlackColor
				}],
				['input[data-selectable]', {
					backgroundColor: 'transparent',
					border: 'none',
					color: 'inherit',
					textAlign: 'center',
					padding: 0
				}]
			]
		});

		return {
			styleEl: styleEl
		};
	},

	render: function () {
		var redirectURI = window.location.protocol + '//'+ window.location.host + '/oauth/azure';
		return (
			<ol id={this.state.styleEl.id}>
				<li>
					<ExternalLink href="https://manage.windowsazure.com">Sign into the Azure management portal</ExternalLink>
				</li>

				<li>
					<img src={AssetPaths["azure-app-0.png"]} style={{
						width: 300,
						height: 219
					}} />
					<p>Click on the "ACTIVE DIRECTORY" navigation item on the left</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-1.png"]}  style={{
						width: 300,
						height: 238
					}}/>
					<p>Click on "Default Directory"</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-2.png"]}  style={{
						width: 300,
						height: 355
					}}/>
					<img src={AssetPaths["azure-app-2-2.png"]}  style={{
						width: 659,
						height: 102
					}}/>
					<p>Click the "ADD" button at the bottom</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-3.png"]}  style={{
						width: 659,
						height: 498
					}}/>
					<p>Click "Add an application my organization is developing"</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-4.png"]}  style={{
						width: 659,
						height: 471
					}}/>
					<p>Give the application a name such as "flynn-installer"</p>
					<p>Select the "NATIVE CLIENT APPLICATION" option</p>
					<p>Click the arrow in the bottom right of the modal to continue</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-5.png"]}  style={{
						width: 659,
						height: 471
					}}/>
					<p>Set "<input type="text" value={redirectURI} data-selectable onClick={this.__handleRedirectURIInputClick} style={{
							width: Math.ceil(((redirectURI.length * 16) / 2) - 22) + 'px'
						}} />" as the "REDIRECT URI"</p>
					<p>Click the checkmark in the bottom right to continue</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-6.png"]}  style={{
						width: 659,
						height: 278
					}}/>
					<label>
						<p>Click the "CONFIGURE" tab</p>
						<p>Copy the "CLIENT ID" into the input below</p>
						<input name="client_id" type="text" placeholder="CLIENT ID" />
					</label>
				</li>

				<li>
					<img src={AssetPaths["azure-app-7.png"]}  style={{
						width: 659,
						height: 155
					}}/>
					<p>Scroll to the bottom of the configuration page</p>
					<p>Click the green "Add application" button</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-9.png"]}  style={{
						width: 659,
						height: 415
					}}/>
					<p>Click the "Windows Azure Service ..." option</p>
					<p>Click the checkmark in the bottom right to continue</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-10.png"]}  style={{
						width: 659,
						height: 169
					}}/>
					<p>Click the "Delegated Permissions" dropdown</p>
					<p>Check "Access Azure Service Management"</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-11.png"]}  style={{
						width: 659,
						height: 93
					}}/>
					<p>Click the "Save" button</p>
				</li>

				<li>
					<img src={AssetPaths["azure-app-12.png"]}  style={{
						width: 659,
						height: 372
					}}/>
					<label>
						<p>Click on the back arrow button to go back to the "APPLICATIONS" tab click and the "ENDPOINTS" button at the bottom</p>
						<p>Copy your OAuth 2.0 Token Endpoint into the input below</p>
						<input name="endpoint" type="text" placeholder="https://login.microsoftonline.com/{your-uid}/oauth2/token?api-version=1.0" />
					</label>
				</li>
			</ol>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
	},

	__handleRedirectURIInputClick: function (e) {
		InputSelection.selectAll(e.target);
	}
});

export default AzureCreateAppTutorial;
