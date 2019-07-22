import * as React from 'react';
import useClient from './useClient';
import useMergeDispatch from './useMergeDispatch';
import { setNameFilters, setPageSize } from './client';
import { Release, StreamReleasesResponse } from './generated/controller_pb';

export enum ActionType {
	SET_RELEASE = 'useRelease__SET_RELEASE',
	SET_ERROR = 'useRelease__SET_ERROR',
	SET_LOADING = 'useRelease__SET_LOADING'
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

export function useReleaseWithDispatch(releaseName: string, callerDispatch: Dispatcher) {
	const client = useClient();
	const [, localDispatch] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);
	React.useEffect(() => {
		// support being called with empty name
		// (see <CreateDeployment />)
		if (!releaseName) {
			dispatch([
				{ type: ActionType.SET_RELEASE, release: null },
				{ type: ActionType.SET_ERROR, error: null },
				{ type: ActionType.SET_LOADING, loading: false }
			]);
			return;
		}
		const cancel = client.streamReleases(
			(res: StreamReleasesResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				dispatch([
					{ type: ActionType.SET_RELEASE, release: res.getReleasesList()[0] || null },
					{ type: ActionType.SET_ERROR, error: null },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			},
			setNameFilters(releaseName),
			setPageSize(1)
		);
		return cancel;
	}, [releaseName, client, dispatch]);
}

export default function useRelease(releaseName: string) {
	const [{ release, error, loading }, dispatch] = React.useReducer(reducer, initialState());
	useReleaseWithDispatch(releaseName, dispatch);
	return {
		release,
		error,
		loading
	};
}
