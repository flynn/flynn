import UserAgent from './css/user-agent';
import { green as GreenBtnCSS } from './css/button';
import AssetPaths from './asset-paths';

var firefoxImgInfo = {
	width: 600,
	height: 307,
	name: 'osx-firefox-cert-1'
};

if (UserAgent.isWindows()) {
	firefoxImgInfo = {
		width: 600,
		height: 336,
		name: 'windows-firefox-cert-1'
	};
} else if (UserAgent.isLinux()) {
	firefoxImgInfo = {
		width: 600,
		height: 328,
		name: 'linux-firefox-cert-1'
	};
}

var FirefoxSteps = React.createClass({
	render: function () {
		var imgInfo = firefoxImgInfo;
		return (
			<ol>
				<li>
					<p>
						<a href={this.props.certURL}>Click here</a> and accept the prompt to install the certificate.
					</p>
					<p>
						<strong>Make sure you check the "Trust this CA to identify websites" box:</strong>
					</p>
					<p>
						<img
							alt="Trust this CA to identify websites"
							src={AssetPaths[imgInfo.name +'.png']}
							style={{
								width: imgInfo.width,
								height: imgInfo.height
							}} />
					</p>
				</li>

				<li>
					<button type="submit" style={GreenBtnCSS}>All done</button>
				</li>
			</ol>
		);
	}
});

var KeychainOSXSteps = React.createClass({
	render: function () {
		return (
			<ol>
				<li>
					<p>
						<a href={this.props.certURL}>Click here</a> to download the certificate.
					</p>
				</li>

				<li>
					<p>Open the downloaded certificate and click "Always Trust":</p>
					<p>
						<img
							src={AssetPaths['osx-cert-2.png']}
							alt="Install certificate in Keychain app"
							style={{
								width: 410,
								height: 400
							}}/>
					</p>
				</li>

				<li>
					<button type="submit" style={GreenBtnCSS}>All done</button>
				</li>
			</ol>
		);
	}
});

var ChromeLinuxSteps = React.createClass({
	render: function () {
		return (
			<ol>
				<li>
					<p>
						<a href={this.props.certURL}>Click here</a> to download the certificate.
					</p>
				</li>

				<li>
					<p>
						<img
							src={AssetPaths['linux-chrome-cert-1.png']}
							alt="Open your browser settings"
							style={{
								width: 200,
								height: 384
							}} />
					</p>
				</li>

				<li>
					<p>Click "Manage certificates...":</p>
					<p>
						<img
							src={AssetPaths['linux-chrome-cert-2.png']}
							alt="Search for SSL"
							style={{
								width: 600,
								height: 133
							}} />
					</p>
				</li>

				<li>
					<p>Click "Import" under the "Authorities" tab:</p>
					<p>
						<img
							src={AssetPaths['linux-chrome-cert-3.png']}
							alt="Certificate manager"
							style={{
								width: 600,
								height: 428
							}} />
					</p>
				</li>

				<li>
					<p>Select the downloaded certificate file:</p>
					<p>
						<img
							src={AssetPaths['linux-chrome-cert-4.png']}
							alt="File browser"
							style={{
								width: 600,
								height: 464
							}} />
					</p>
				</li>

				<li>
					<p>Check "Trust this certificate for identifying websites" and click "OK":</p>
					<p>
						<img
							src={AssetPaths['linux-chrome-cert-5.png']}
							alt="Trust certificate"
							style={{
								width: 400,
								height: 253
							}} />
					</p>
				</li>

				<li>
					<button type="submit" style={GreenBtnCSS}>All done</button>
				</li>
			</ol>
		);
	}
});

var ChromeWindowsSteps = React.createClass({
	render: function () {
		return (
			<ol>
				<li>
					<p>
						<a href={this.props.certURL}>Click here</a> to download the certificate.
					</p>
				</li>

				<li>
					<p>Open the downloaded certificate:</p>
					<p>
						<img
							src={AssetPaths['windows-cert-1.png']}
							alt="Open the downloaded certificate"
							style={{
								width: 300,
								height: 219
							}} />
					</p>
				</li>

				<li>
					<p>Click "Install certificate...":</p>
					<p>
						<img
							src={AssetPaths['windows-cert-2.png']}
							alt="Install the certificate"
							style={{
								width: 300,
								height: 374
							}} />
					</p>
				</li>

				<li>
					<p>
						<img
							src={AssetPaths['windows-cert-3.png']}
							alt="Welcome to the Certificate Import Wizard"
							style={{
								width: 400,
								height: 390
							}} />
					</p>
				</li>

				<li>
					<p><strong>Select "Trusted Root Certification Authorities":</strong></p>
					<p>
						<img
							src={AssetPaths['windows-cert-4.png']}
							alt="Trusted Root Certification Authorities"
							style={{
								width: 400,
								height: 390
							}} />
					</p>
				</li>

				<li>
					<p>
						<img
							src={AssetPaths['windows-cert-5.png']}
							alt="Completing the Certificate Import Wizard"
							style={{
								width: 400,
								height: 390
							}} />
					</p>
				</li>

				<li>
					<p><strong>Accept the Security Warning:</strong></p>
					<p>
						<img
							src={AssetPaths['windows-cert-6.png']}
							alt="Security Warning"
							style={{
								width: 400,
								height: 331
							}} />
					</p>
				</li>

				<li>
					<p>
						<img
							src={AssetPaths['windows-cert-7.png']}
							alt="The import was successful"
							style={{
								width: 200,
								height: 132
							}} />
					</p>
				</li>

				<li>
					Restart your browser.
				</li>

				<li>
					<button type="submit" style={GreenBtnCSS}>All done</button>
				</li>
			</ol>
		);
	}
});

var UnknownBrowserSteps = React.createClass({
	render: function () {
		return (
			<ol>
				<li>
					<p>
						<a href={this.props.certURL}>Click here</a> to download the certificate.
					</p>
				</li>

				<li>
					<p>
						Install the certificate.
					</p>
				</li>

				<li>
					<button type="submit" style={GreenBtnCSS}>All done</button>
				</li>
			</ol>
		);
	}
});

var StepsComponent = UnknownBrowserSteps;
if (UserAgent.isFirefox()) {
	StepsComponent = FirefoxSteps;
} else if ((UserAgent.isChrome() || UserAgent.isSafari()) && UserAgent.isOSX()) {
	StepsComponent = KeychainOSXSteps;
} else if (UserAgent.isChrome() && UserAgent.isLinux()) {
	StepsComponent = ChromeLinuxSteps;
} else if (UserAgent.isChrome() && UserAgent.isWindows()) {
	StepsComponent = ChromeWindowsSteps;
}

var InstallCert = React.createClass({
	render: function () {
		return (
			<section>
				<header>
					<h1>Install CA certificate to continue</h1>
					<p>
						This CA certificate allows you to access the dashboard securely.
						The certificate was generated as part of the installation process, and the private key has already been discarded.
						The only certificates it has signed are for the cluster domain, and no more certificates can be signed by it.
					</p>
				</header>

				<StepsComponent
					certURL={this.props.certURL} />
			</section>
		);
	}
});

export default InstallCert;
