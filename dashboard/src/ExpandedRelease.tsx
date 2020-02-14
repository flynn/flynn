import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box, Button } from 'grommet';
import { Release } from './generated/controller_pb';
import KeyValueDiff from './KeyValueDiff';
import ExternalAnchor from './ExternalAnchor';
import ReleaseProcessesDiff from './ReleaseProcessesDiff';
import isActionType from './util/isActionType';
import useApp from './useApp';
import useMergeDispatch from './useMergeDispatch';
import {
	useAppReleaseWithDispatch,
	State as AppReleaseState,
	Action as AppReleaseAction,
	ActionType as AppReleaseActionType,
	initialState as initialAppReleaseState,
	reducer as appReleaseReducer
} from './useAppRelease';

export enum ActionType {
	// parent component should handle these actions
	DEPLOY_RELEASE = 'ExpandedRelease__DEPLOY_RELEASE',
	CLOSE = 'ExpandedRelease__CLOSE'
}

interface DeployReleaseAction {
	type: ActionType.DEPLOY_RELEASE;
	releaseName: string;
}

interface CloseAction {
	type: ActionType.CLOSE;
}

export type Action = DeployReleaseAction | CloseAction | AppReleaseAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	// useAppRelease
	currentReleaseState: AppReleaseState;
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function initialState(): State {
	return {
		// useAppRelease
		currentReleaseState: initialAppReleaseState()
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
				// useAppRelease
				if (isActionType<AppReleaseAction>(AppReleaseActionType, action)) {
					nextState.currentReleaseState = appReleaseReducer(prevState.currentReleaseState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	return nextState;
}

interface Props {
	appName: string;
	release: Release;
	prevRelease?: Release;
	dispatch: Dispatcher;
}

export default function ExpandedRelease({ appName, release, prevRelease: prev, dispatch: callerDispatch }: Props) {
	const [
		{
			currentReleaseState: { release: currentRelease }
		},
		localDispatch
	] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);
	useAppReleaseWithDispatch(appName, dispatch);

	const { app } = useApp(appName);

	const releaseID = release.getName().replace(appName + '/releases/', '');
	const createTime = ((ts) => (ts ? ts.toDate() : null))(release.getCreateTime());

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
		if (prev) {
			githubCompareURL = `${baseGithubURL}/compare/${gitCommit(prev)}...${gitCommit(release)}`;
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
			dispatch({ type: ActionType.CLOSE });
		},
		[dispatch]
	);

	return (
		<Box tag="form" fill direction="column" onSubmit={handleSubmit} gap="small" justify="between">
			<Box>
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
				<ReleaseProcessesDiff release={release} prevRelease={prev} />
				<h3>Environment Variables</h3>
				<KeyValueDiff prev={prev ? prev.getEnvMap() : new jspb.Map([])} next={release.getEnvMap()} />
				<h3>Metadata</h3>
				<KeyValueDiff prev={prev ? prev.getLabelsMap() : new jspb.Map([])} next={release.getLabelsMap()} />
			</Box>
			<Box fill="horizontal" direction="row" align="end" gap="small" justify="between">
				<Button
					type="submit"
					disabled={!!currentRelease && release.getName() === currentRelease.getName()}
					primary
					label="Rollback to release"
				/>
				<Button type="button" label="Close" onClick={handleCloseBtnClick} />
			</Box>
		</Box>
	);
}
