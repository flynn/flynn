import * as React from 'react';
import useClient from './useClient';
import useMergeDispatch from './useMergeDispatch';
import { setNameFilters, setPageSize, setStreamCreates, setStreamUpdates, setDeploymentStatusFilters } from './client';
import { Release, DeploymentStatus, StreamDeploymentsResponse } from './generated/controller_pb';

export enum ActionType {
	SET_RELEASE = 'useAppRelease__SET_RELEASE',
	SET_ERROR = 'useAppRelease__SET_ERROR',
	SET_LOADING = 'useAppRelease__SET_LOADING'
}

interface SetReleaseAction {
	type: ActionType.SET_RELEASE;
	release: Release | null;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error | null;
}

interface SetLoadingAction {
	type: ActionType.SET_LOADING;
	loading: boolean;
}

export type Action = SetReleaseAction | SetErrorAction | SetLoadingAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	release: Release | null;
	error: Error | null;
	loading: boolean;
}

export function initialState(): State {
	return {
		release: null,
		error: null,
		loading: true
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	return actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_RELEASE:
				nextState.release = action.release;
				return nextState;

			case ActionType.SET_ERROR:
				nextState.error = action.error;
				return nextState;

			case ActionType.SET_LOADING:
				nextState.loading = action.loading;
				return nextState;

			default:
				return prevState;
		}
	}, prevState);
}

export function useAppReleaseWithDispatch(appName: string, callerDispatch: Dispatcher) {
	const client = useClient();
	const [, localDispatch] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);
	React.useEffect(() => {
		const callback = (res: StreamDeploymentsResponse, error: Error | null) => {
			if (error) {
				dispatch([
					{ type: ActionType.SET_ERROR, error },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
				return;
			}
			const deployment = res.getDeploymentsList()[0];
			if (deployment) {
				if (deployment.getStatus() === DeploymentStatus.COMPLETE) {
					dispatch([
						{ type: ActionType.SET_RELEASE, release: deployment.getNewRelease() || new Release() },
						{ type: ActionType.SET_ERROR, error: null },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
				}
			} else {
				dispatch([
					{ type: ActionType.SET_RELEASE, release: new Release() },
					{ type: ActionType.SET_ERROR, error: null },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			}
		};
		const cancel = client.streamDeployments(
			callback,
			setNameFilters(appName),
			setDeploymentStatusFilters(DeploymentStatus.COMPLETE),
			setPageSize(1),
			setStreamCreates(),
			setStreamUpdates()
		);
		return cancel;
	}, [appName, client, dispatch]);
}

export default function useAppRelease(appName: string) {
	const [{ release, error, loading }, dispatch] = React.useReducer(reducer, initialState());
	useAppReleaseWithDispatch(appName, dispatch);
	return {
		release,
		error,
		loading
	};
}
