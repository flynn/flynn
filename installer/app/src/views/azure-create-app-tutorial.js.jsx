import AssetPaths from './asset-paths';
import ExternalLink from './external-link';
import Sheet from './css/sheet';
import InputSelection from './input-selection';

var tutorialSlides = [];

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-1.gif"]} style={{
			width: 699,
			height: 378
		}} />
		<p><ExternalLink href="https://manage.windowsazure.com">Sign into the Azure Management Portal</ExternalLink> and select the "Active Directory" navigation item on the left.</p>
	</div>
);

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-2.gif"]} style={{
			width: 699,
			height: 378
		}} />
		<p>Click on "Default Directory" (or the one you want to use) and select the "Applications" navigation tab.</p>
	</div>
);

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-3.gif"]} style={{
			width: 699,
			height: 378
		}} />
		<p>Click the "Add" button at the bottom and select "Add an application my organization is developing". For the name, choose "flynn-installer" (or something else), then select "Native Client Application".</p>
	</div>
);

var redirectURISlideIndex = tutorialSlides.length;
tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-4.gif"]} style={{
			width: 699,
			height: 378
		}} />
		<p>As Redirect URI, use the value below, and create the application by hitting the checkmark in the lower right.</p>
	</div>
);

var clientIDSlideIndex = tutorialSlides.length;
tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-5.gif"]} style={{
			width: 699,
			height: 378
		}} />
		<p>Click the "Configure" tab and copy the "Client ID" into the input field below.</p>
	</div>
);

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-6.gif"]} style={{
			width: 699,
			height: 402
		}} />
		<p>Next, we need to allow the created app to control your Azure account. Scroll to the bottom of the configuration page and click the green "Add application" button. In the popup, select the "Windows Azure Service Management API" and click the checkmark in the lower right.</p>
	</div>
);

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-7.gif"]} style={{
			width: 699,
			height: 402
		}} />
		<p>Click the "Delegated Permissions" dropdown and check "Access Azure Service Management". Then, save the configuration.</p>
	</div>
);

var tokenEndpointSlideIndex = tutorialSlides.length;
tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-8.gif"]} style={{
			width: 699,
			height: 472
		}} />
		<p>Click on the back arrow button to go back to the "APPLICATIONS" tab and click and the "ENDPOINTS" button at the bottom. Then, copy your OAuth 2.0 Token Endpoint into the input below.</p>
	</div>
);

tutorialSlides.push(
	<div>
		<img src={AssetPaths["azure-done.png"]} style={{
			width: 699,
			height: 256
		}} />
		<p>You now created an appliation able to control your Azure resources - one that this Flynn installer can use. You can connect this installer and your new app by clicking "Authenticate" below - lets install Flynn!</p>
	</div>
);

var AzureCreateAppTutorial = React.createClass({
	getInitialState: function () {
		var styleEl = Sheet.createElement({
			marginTop: '1rem',
			selectors: [
				['> li > img', {
					verticalAlign: 'text-top'
				}],
				['input[data-selectable]', {
					backgroundColor: 'transparent',
					border: 'none',
					color: 'inherit',
					textAlign: 'center',
					padding: 0
				}],
				['input', {
					margin: '0 0 5px 0'
				}]
			]
		});

		return {
			styleEl: styleEl,
			slideIndex: null
		};
	},

	render: function () {
		var redirectURI = window.location.protocol + '//'+ window.location.host + '/oauth/azure';
		var inputStyles = this.__getInputStyles();
		var state = this.state;
		var pagination = {
			prev: null,
			next: null,
			submit: null
		};
		var intro = null;
		var lastSlideIndex = tutorialSlides.length-1;

		intro = (
			<div>
				<p>To use the installer, you first need to create an Azure application able to control your resources on Flynns behalf.</p>
				<button onClick={this.__handleAdvanceTutorialClick}>Walk me through it</button>
				<br />
				<button onClick={this.__skipTutorial}>Skip tutorial</button>
			</div>
		);

		if (state.slideIndex !== null || state.skipTutorial) {
			intro = null;
			if (state.skipTutorial && state.slideIndex === null) {
				pagination.prev = (
					<button type="text" onClick={this.__skipTutorial}>Back</button>
				);
			}
			if (state.slideIndex !== null) {
				pagination.prev = (
					<button type="text" onClick={this.__handleGoBackTutorialClick}>Back</button>
				);
			}
			if (state.slideIndex !== null && state.slideIndex < lastSlideIndex) {
				pagination.next = (
					<button type="text" onClick={this.__handleAdvanceTutorialClick}>Next</button>
				);
			}
			if (state.skipTutorial || state.slideIndex === lastSlideIndex) {
				pagination.submit = (
					<button type="submit" style={{float: 'right'}}>Authenticate</button>
				);
			}
		}

		return (
			<div id={this.state.styleEl.id}>
				{intro}
				{state.slideIndex !== null ? tutorialSlides[state.slideIndex] : null}

				<div>
					<label style={inputStyles.redirectURI}>
						<div>Redirect URI</div>
						<input
							ref="redirectURI"
							name="redirectURI"
							type="text"
							value={redirectURI}
							onClick={this.__handleRedirectURIInputClick} />
					</label>

					<label style={inputStyles.clientId}>
						<div>App Client ID</div>
						<input
							ref="client_id"
							name="client_id"
							type="text"
							placeholder="ab7c1052-1fe7-4642-91f6-065c94de25d4" />
					</label>

					<label style={inputStyles.endpoint}>
						<div>OAuth 2.0 Token Endpoint</div>
						<input
							ref="endpoint"
							name="endpoint"
							type="text"
							placeholder="https://login.microsoftonline.com/{your-uid}/oauth2/token?api-version=1.0" />
					</label>
				</div>

				{pagination.prev} {pagination.next} {pagination.submit}
			</div>
		);
	},

	componentDidMount: function () {
		this.state.styleEl.commit();
	},

	/**
	 * Modifies the state's `slideIndex` variable, advancing in the tutorial's walkthrough.
	 */
	__handleAdvanceTutorialClick: function (e) {
		e.preventDefault();
		var prevState = this.state;

		// Validate Inputs
		var clientIDInput = this.refs.client_id.getDOMNode();
		if (prevState.slideIndex === clientIDSlideIndex && clientIDInput.value === '') {
			clientIDInput.focus();
			return;
		}
		var endpointInput = this.refs.endpoint.getDOMNode();
		if (prevState.slideIndex === tokenEndpointSlideIndex && endpointInput.value === '') {
			endpointInput.focus();
			return;
		}

		var slideIndex = null;
		if (prevState.slideIndex === null || prevState.slideIndex >= tutorialSlides.length) {
			slideIndex = 0;
		} else {
			slideIndex = prevState.slideIndex + 1;
		}


		this.setState({
			slideIndex: slideIndex,
			showRedirectURI: (slideIndex === redirectURISlideIndex),
			showClientIDInput: (slideIndex === clientIDSlideIndex),
			showEndpointInput: (slideIndex === tokenEndpointSlideIndex),
			done: (slideIndex === tutorialSlides.length || prevState.skipTutorial)
		});
	},

	__handleGoBackTutorialClick: function (e) {
		e.preventDefault();
		var prevState = this.state;
		var slideIndex = prevState.slideIndex - 1;
		if (slideIndex < 0) {
			slideIndex = null;
		}
		this.setState({
			slideIndex: slideIndex,
			showRedirectURI: (slideIndex === redirectURISlideIndex),
			showClientIDInput: (slideIndex === clientIDSlideIndex),
			showEndpointInput: (slideIndex === tokenEndpointSlideIndex)
		});
	},

	/**
	 * Select's the current's input contents for easier copying.
	 * @param  {event} e
	 */
	__handleRedirectURIInputClick: function (e) {
		InputSelection.selectAll(e.target);
	},

	/**
	 * Returns the styles the individual input fields, depending on whether or not they should
	 * be visible or not.
	 * @return {object} A on object with three keys (redirectURI, clientID, endpoint) with styles
	 */
	__getInputStyles: function () {
		var state = this.state;
		var hiddenStyle = {display: 'none', visibility: 'collapse'};
		var redirectURI = (state.skipTutorial || state.showRedirectURI) ? {} : hiddenStyle;
		var clientId = (state.skipTutorial || state.showClientIDInput) ? {} : hiddenStyle;
		var endpoint = (state.skipTutorial || state.showEndpointInput) ? {} : hiddenStyle;

		return {
			redirectURI: redirectURI,
			clientId: clientId,
			endpoint: endpoint
		};
	},

	/**
	 * Skips the tutorial. The tutorial is already skipped and this method is called again, it
	 * resets the tutorial.
	 */
	__skipTutorial: function () {
		if (this.state.skipTutorial) {
			return this.setState({skipTutorial: null});
		} else {
			this.setState({
				skipTutorial: true,
				tutorialSlide: null
			});
		}
	}
});

export default AzureCreateAppTutorial;
