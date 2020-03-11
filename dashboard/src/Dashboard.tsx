import * as React from 'react';
import { BrowserRouter as Router, Switch, Route } from 'react-router-dom';

import { Grommet, Box, Heading } from 'grommet';
import styled from 'styled-components';

import theme from './theme';
import { useLocation } from 'react-router-dom';
import Split from './Split';
import Loading from './Loading';
import AppsListNav from './AppsListNav';
import { DisplayErrors } from './useErrorHandler';
import flynnLogoPath from './flynn.svg';
import useOAuth from './useOAuth';

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

const AppComponent = React.lazy(() => import('./AppComponent'));

const StyledLogoImg = styled('img')`
	height: 2em;
`;

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
	const { authenticated } = useOAuth();

	return (
		<Split>
			<Box tag="aside" basis="medium" flex={false} fill>
				<Box tag="header" pad="small" direction="row">
					<StyledLogoImg src={flynnLogoPath} alt="Flynn Logo" />
				</Box>
				<Box flex>{authenticated ? <AppsListNav /> : null}</Box>
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
						<Loading />
					)}
				</React.Suspense>
			</Box>
		</Split>
	);
}

export default function Dashboard() {
	return (
		<Grommet full theme={theme} cssVars>
			<Router>
				<React.StrictMode>
					<DashboardInner />
				</React.StrictMode>
			</Router>
		</Grommet>
	);
}
