import * as React from 'react';
import { BrowserRouter as Router, Switch, Route } from 'react-router-dom';

import { Grommet, Box, Paragraph, Heading, Button } from 'grommet';
import { aruba } from 'grommet-theme-aruba';
import tinycolor from 'tinycolor2';

import Config from './config';
import useClient from './useClient';
import useWithCancel from './useWithCancel';
import { useLocation } from 'react-router-dom';
import Split from './Split';
import Loading from './Loading';
import AppsListNav from './AppsListNav';
import ExternalAnchor from './ExternalAnchor';
import { DisplayErrors } from './useErrorHandler';

// DEBUG:
import { default as client, Client } from './client';
declare global {
	interface Window {
		client: Client;
	}
}
if (typeof window !== 'undefined') {
	window.client = client;
}

const Login = React.lazy(() => import('./Login'));
const AppComponent = React.lazy(() => import('./AppComponent'));

function appNameFromPath(path: string): string {
	const m = path.match(/\/apps\/[^/]+/);
	return m ? m[0].slice(1) : '';
}

/*
 * <Dashboard> is the root component of the dashboard app
 */
function DashboardInner() {
	const location = useLocation();
	const currentPath = React.useMemo(() => location.pathname || '', [location.pathname]);
	const [appName, setAppName] = React.useState<string>(appNameFromPath(currentPath));
	React.useEffect(() => {
		setAppName(appNameFromPath(currentPath));
	}, [currentPath]);
	const client = useClient();
	const withCancel = useWithCancel();
	const [authenticated, setAuthenticated] = React.useState(Config.isAuthenticated());

	const handleLogoutBtnClick = (e: React.SyntheticEvent) => {
		e.preventDefault();
		const cancel = client.logout(() => {
			setAuthenticated(false);
		});
		withCancel.set('logout', cancel);
	};

	return (
		<Split>
			<Box tag="aside" basis="medium" flex={false} background="neutral-1" fill>
				<Box tag="header" pad="small">
					<h1>Flynn Dashboard</h1>
				</Box>
				<Box flex>{authenticated ? <AppsListNav /> : null}</Box>
				<Box tag="footer" alignSelf="center">
					<Paragraph size="small" margin="xsmall">
						Flynn is designed, built, and managed by Prime Directive, Inc.
						<br />
						&copy; 2013-
						{new Date().getFullYear()} Prime Directive, Inc. Flynn® is a trademark of Prime Directive, Inc.
					</Paragraph>
					<Paragraph size="small" margin="xsmall">
						<ExternalAnchor href="https://flynn.io/legal/privacy">Privacy Policy</ExternalAnchor>
						&nbsp;|&nbsp;
						<ExternalAnchor href="https://flynn.io/docs/trademark-guidelines">Trademark Guidelines</ExternalAnchor>
						{authenticated ? null : (
							<>
								&nbsp;|&nbsp;
								<Button plain as="a" onClick={handleLogoutBtnClick}>
									Logout
								</Button>
							</>
						)}
					</Paragraph>
				</Box>
			</Box>

			<Box pad="xsmall" fill overflow="scroll" gap="small">
				<DisplayErrors />
				<React.Suspense fallback={<Loading />}>
					{authenticated ? (
						<Switch>
							<Route path="/apps/:appID">
								<AppComponent key={appName} name={appName} />
							</Route>
							<Route path="/">
								<Heading>Select an app to begin.</Heading>
							</Route>
						</Switch>
					) : (
						<Login onLoginSuccess={() => setAuthenticated(true)} />
					)}
				</React.Suspense>
			</Box>
		</Split>
	);
}

const colors = {
	green: '#1bb45e',
	blue: '#08a1f4',
	orangeLight: '#ff7700',
	gray: '#727272',
	black: '#000000',
	white: '#ffffff'
};
const modifiedAruba = Object.assign({}, aruba, {
	global: Object.assign({}, (aruba as any).global, {
		font: {
			family: null // inherit from ./index.css
		},
		colors: Object.assign({}, (aruba as any).global.colors, {
			// color used on active hover state
			active:
				'#' +
				tinycolor(colors.white)
					.darken(10)
					.toHex(),
			// the main brand color
			brand: colors.green,
			// the color to be used when element is in focus
			focus: colors.blue,
			// the text color of the input placeholder
			placeholder: colors.gray,
			// shade of white
			white: colors.white,
			// shade of black
			black: colors.black,
			'accent-1':
				'#' +
				tinycolor(colors.gray)
					.darken(20)
					.toHex(),
			'status-warning': colors.orangeLight,
			textInput: {
				backgroundColor: colors.white
			},
			border: {
				// default border color for light mode
				light: colors.gray,
				// default border color for dark mode
				dark: colors.white
			},
			control: {
				// default control color for light mode
				light: colors.green,
				// default control color for dark mode
				dark: colors.green
			},
			text: {
				// the default application text color for light mode
				light: colors.gray,
				// the default application text color for dark mode
				dark: colors.white
			}
		})
	})
});

export default function Dashboard() {
	return (
		<Grommet full theme={modifiedAruba} cssVars>
			<Router>
				<React.StrictMode>
					<DashboardInner />
				</React.StrictMode>
			</Router>
		</Grommet>
	);
}
