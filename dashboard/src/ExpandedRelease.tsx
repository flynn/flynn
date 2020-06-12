import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box } from 'grommet';
import Button from './Button';
import { Release, ExpandedDeployment } from './generated/controller_pb';
import Notification from './Notification';
import KeyValueDiff from './KeyValueDiff';
import ExternalAnchor from './ExternalAnchor';
import ReleaseProcessesDiff from './ReleaseProcessesDiff';
import isActionType from './util/isActionType';
import useRouter from './useRouter';
import useDeploymentWithDispatch, {
	Action as DeploymentAction,
	ActionType as DeploymentActionType,
	State as DeploymentState,
	reducer as deploymentReducer,
	initialState as initialDeploymentState
} from './useDeployment';
import {
	useAppWithDispatch,
	Action as AppAction,
	ActionType as AppActionType,
	State as AppState,
	reducer as appReducer,
	initialState as initialAppState
} from './useApp';
import useMergeDispatch from './useMergeDispatch';
import {
	useAppReleaseWithDispatch,
	State as AppReleaseState,
	Action as AppReleaseAction,
	ActionType as AppReleaseActionType,
	initialState as initialAppReleaseState,
	reducer as appReleaseReducer
} from './useAppRelease';
import Loading from './Loading';
import styled from 'styled-components';

// grommet sets min-height to 0 as a flexbox hack but it's a layout problem here
const StyledBox = styled(Box)`
	min-height: unset;
`;

export enum ActionType {
	// parent component should handle these actions
	DEPLOY_RELEASE = 'ExpandedRelease__DEPLOY_RELEASE'
}

interface DeployReleaseAction {
	type: ActionType.DEPLOY_RELEASE;
	releaseName: string;
}

export type Action = DeployReleaseAction | AppAction | AppReleaseAction | DeploymentAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	// useApp
	appState: AppState;

	// useAppRelease
	currentReleaseState: AppReleaseState;

	// useDeployment
	deploymentState: DeploymentState;
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function initialState(): State {
	return {
		// useApp
		appState: initialAppState(),

		// useAppRelease
		currentReleaseState: initialAppReleaseState(),

		// useDeployment
		deploymentState: initialDeploymentState()
	};
}

export function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			default:
				// useApp
				if (isActionType<AppAction>(AppActionType, action)) {
					nextState.appState = appReducer(prevState.appState, action);
					return nextState;
				}

				// useAppRelease
				if (isActionType<AppReleaseAction>(AppReleaseActionType, action)) {
					nextState.currentReleaseState = appReleaseReducer(prevState.currentReleaseState, action);
					return nextState;
				}

				// useDeployment
				if (isActionType<DeploymentAction>(DeploymentActionType, action)) {
					nextState.deploymentState = deploymentReducer(prevState.deploymentState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	return nextState;
}

interface Props {
	dispatch: Dispatcher;
}

export default function ExpandedRelease({ dispatch: callerDispatch }: Props) {
	const {
		urlParams,
		match: { params: matchParams },
		history
	} = useRouter();

	const { appID, deploymentID } = matchParams;
	const appName = `apps/${appID}`;
	const deploymentName = `${appName}/deployments/${deploymentID}`;

	const [
		{
			appState: { app, loading: appLoading, error: appError },
			currentReleaseState: { release: currentRelease, loading: currentReleaseLoading, error: currentReleaseError },
			deploymentState: { deployment: deploymentOrNull, loading: deploymentLoading, error: deploymentError }
		},
		localDispatch
	] = React.useReducer(reducer, initialState());
	const error = appError || currentReleaseError || deploymentError;
	const deployment = deploymentOrNull || new ExpandedDeployment();
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);
	useAppWithDispatch(appName, dispatch);
	useAppReleaseWithDispatch(appName, dispatch);
	useDeploymentWithDispatch(deploymentName, dispatch);

	const release = deployment.getNewRelease() || new Release();
	const prevRelease = deployment.getOldRelease();

	const releaseID = release.getName().replace(appName + '/releases/', '');
	const createTime = ((ts) => (ts ? ts.toDate() : null))(deployment.getCreateTime());

	const labels = release.getLabelsMap();
	const appMeta = app ? app.getLabelsMap() : new jspb.Map([]);

	const gitCommit = (release: Release) => {
		const labels = release.getLabelsMap();
		return (
			labels.get('git.commit') ||
			(() => {
				const rev = labels.get('rev');
				if (labels.get('git') === 'true' && rev) {
					return rev;
				}
				return null;
			})()
		);
	};

	let baseGithubURL = (appMeta.get('github.url') || null) as string | null;
	let githubCompareURL = null as string | null;
	let githubURL = null as string | null;
	if (baseGithubURL) {
		baseGithubURL = baseGithubURL.replace(/\/?$/, '');
	} else if (labels.get('github') === 'true') {
		baseGithubURL = `https://github.com/${labels.get('github_user')}/${labels.get('github_repo')}`;
	}
	if (baseGithubURL) {
		githubURL = `${baseGithubURL}/commit/${gitCommit(release)}`;
		if (prevRelease) {
			githubCompareURL = `${baseGithubURL}/compare/${gitCommit(prevRelease)}...${gitCommit(release)}`;
		}
	}

	const handleSubmit = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			dispatch({ type: ActionType.DEPLOY_RELEASE, releaseName: release.getName() });
		},
		[release, dispatch]
	);

	const handleCloseBtnClick = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			history.push({ pathname: `/${appName}`, search: urlParams.toString() });
		},
		[appName, urlParams, history]
	);

	if (appLoading || currentReleaseLoading || deploymentLoading) return <Loading />;

	if (error) {
		return <Notification message={error.message} status="warning" margin="small" />;
	}

	return (
		<Box tag="form" fill direction="column" onSubmit={handleSubmit} gap="small" justify="between">
			<StyledBox>
				Release {releaseID}
				{currentRelease && release.getName() === currentRelease.getName() ? <>&nbsp;(CURRENT)</> : null}
				{createTime ? (
					<>
						<br />
						{createTime.toString()}
					</>
				) : null}
				{gitCommit(release) ? (
					<>
						<span>
							<br />
							git.commit{' '}
							{githubURL ? <ExternalAnchor href={githubURL}>{gitCommit(release)}</ExternalAnchor> : gitCommit(release)}
							{githubCompareURL ? (
								<>
									&nbsp;
									<ExternalAnchor href={githubCompareURL}>[compare]</ExternalAnchor>
								</>
							) : null}
						</span>
						<br />
					</>
				) : null}
				<h3>Processes</h3>
				<ReleaseProcessesDiff release={release} prevRelease={prevRelease} />
				<h3>Environment Variables</h3>
				<KeyValueDiff
					prev={prevRelease ? prevRelease.getEnvMap() : new jspb.Map([])}
					next={release.getEnvMap()}
					showAll={true}
				/>
				<h3>Metadata</h3>
				<KeyValueDiff
					prev={prevRelease ? prevRelease.getLabelsMap() : new jspb.Map([])}
					next={release.getLabelsMap()}
					showAll={true}
				/>
			</StyledBox>
			<StyledBox
				fill="horizontal"
				direction="row"
				align="end"
				gap="small"
				justify="between"
				margin={{ bottom: 'small' }}
			>
				<Button
					type="submit"
					disabled={!!currentRelease && release.getName() === currentRelease.getName()}
					primary
					label="Rollback to release"
				/>
				<Button type="button" label="Close" onClick={handleCloseBtnClick} />
			</StyledBox>
		</Box>
	);
}
