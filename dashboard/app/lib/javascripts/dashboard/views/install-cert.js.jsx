/** @jsx React.DOM */

(function () {

"use strict";

Dashboard.Views.InstallCert = React.createClass({
	displayName: "Views.InstallCert",

	render: function () {
		return (
			<section className="panel">
				<section>
					<header>
						<h1>Install CA certificate to continue</h1>
					</header>

					{this.props.browserName === "firefox" ? (
						<ol>
							<li>
								<p>
									<a href={this.props.certURL}>Click here</a> and accept the prompt to install the certificate.
								</p>
								<p>
                  <strong>Make sure you check the "Trust this CA to identify websites" box:</strong>
								</p>
								<p>
                  {this.props.osName === "windows" ? (
                    <img id="windows-firefox-cert-1" src={Dashboard.config.ASSET_PATHS['windows-firefox-cert-1.png']} alt="Trust this CA to identify websites" />
                  ) : null}

                  {this.props.osName === "linux" ? (
                    <img id="linux-firefox-cert-1" src={Dashboard.config.ASSET_PATHS['linux-firefox-cert-1.png']} alt="Trust this CA to identify websites" />
                  ) : null}

                  {this.props.osName === "osx" || this.props.osName === "unknown" ? (
                    <img id="osx-firefox-cert-1" src={Dashboard.config.ASSET_PATHS['osx-firefox-cert-1.png']} alt="Trust this CA to identify websites" />
                  ) : null}
								</p>
							</li>

							<li>
								<a href="" className="btn-green">All done</a>
							</li>
						</ol>
					) : null}

          {(this.props.browserName === "chrome" || this.props.browserName === "safari") && this.props.osName === "osx" ? (
						<ol>
							<li>
								<p>
									<a href={this.props.certURL}>Click here</a> to download the certificate.
								</p>
							</li>

              <li>
                <p>Open the downloaded certificate and click "Always Trust":</p>
                <p><img id="osx-cert-2" src={Dashboard.config.ASSET_PATHS['osx-cert-2.png']} alt="Install certificate in Keychain app" /></p>
              </li>

							<li>
								<a href="" className="btn-green">All done</a>
							</li>
						</ol>
          ) : null}

          {this.props.browserName === "chrome" && this.props.osName === "linux" ? (
						<ol>
							<li>
								<p>
									<a href={this.props.certURL}>Click here</a> to download the certificate.
								</p>
							</li>

              <li>
                <p><img id="linux-chrome-cert-1" src={Dashboard.config.ASSET_PATHS['linux-chrome-cert-1.png']} alt="Open your browser settings" /></p>
              </li>

              <li>
                <p>Click "Manage certificates...":</p>
                <p><img id="linux-chrome-cert-2" src={Dashboard.config.ASSET_PATHS['linux-chrome-cert-2.png']} alt="Search for SSL" /></p>
              </li>

              <li>
                <p>Click "Import" under the "Authorities" tab:</p>
                <p><img id="linux-chrome-cert-3" src={Dashboard.config.ASSET_PATHS['linux-chrome-cert-3.png']} alt="Certificate manager" /></p>
              </li>

              <li>
                <p>Select the downloaded certificate file:</p>
                <p><img id="linux-chrome-cert-4" src={Dashboard.config.ASSET_PATHS['linux-chrome-cert-4.png']} alt="File browser" /></p>
              </li>

              <li>
                <p>Check "Trust this certificate for identifying websites" and click "OK":</p>
                <p><img id="linux-chrome-cert-5" src={Dashboard.config.ASSET_PATHS['linux-chrome-cert-5.png']} alt="Trust certificate" /></p>
              </li>

							<li>
								<a href="" className="btn-green">All done</a>
							</li>
						</ol>
          ) : null}

          {this.props.browserName === "chrome" && this.props.osName === "windows" ? (
						<ol>
							<li>
								<p>
									<a href={this.props.certURL}>Click here</a> to download the certificate.
								</p>
							</li>

              <li>
                <p>Open the downloaded certificate:</p>
                <p><img id="windows-cert-1" src={Dashboard.config.ASSET_PATHS['windows-cert-1.png']} alt="Open the downloaded certificate" /></p>
              </li>

              <li>
                <p>Click "Install certificate...":</p>
                <p><img id="windows-cert-2" src={Dashboard.config.ASSET_PATHS['windows-cert-2.png']} alt="Install the certificate" /></p>
              </li>

              <li>
                <p><img id="windows-cert-3" src={Dashboard.config.ASSET_PATHS['windows-cert-3.png']} alt="Welcome to the Certificate Import Wizard" /></p>
              </li>

              <li>
                <p><strong>Select "Trusted Root Certification Authorities":</strong></p>
                <p><img id="windows-cert-4" src={Dashboard.config.ASSET_PATHS['windows-cert-4.png']} alt="Trusted Root Certification Authorities" /></p>
              </li>

              <li>
                <p><img id="windows-cert-5" src={Dashboard.config.ASSET_PATHS['windows-cert-5.png']} alt="Completing the Certificate Import Wizard" /></p>
              </li>

              <li>
                <p><strong>Accept the Security Warning:</strong></p>
                <p><img id="windows-cert-6" src={Dashboard.config.ASSET_PATHS['windows-cert-6.png']} alt="Security Warning" /></p>
              </li>

              <li>
                <p><img id="windows-cert-7" src={Dashboard.config.ASSET_PATHS['windows-cert-7.png']} alt="The import was successful" /></p>
              </li>

							<li>
                Restart your browser.
							</li>
						</ol>
          ) : null}

          {this.props.browserName === "unknown" ? (
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
								<a href="" className="btn-green">All done</a>
							</li>
						</ol>
          ) : null}
				</section>
			</section>
		);
	}
});

})();
