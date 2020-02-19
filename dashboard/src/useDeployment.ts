import * as React from 'react';
import useClient from './useClient';
import { setNameFilters, setPageSize, setStreamCreates, setStreamUpdates } from './client';
import { ExpandedDeployment, StreamDeploymentsResponse } from './generated/controller_pb';

export enum ActionType {
	SET_DEPLOYMENT = 'useDeployment__SET_DEPLOYMENT',
	SET_ERROR = 'useDeployment__SET_ERROR',
	SET_LOADING = 'useDeployment__SET_LOADING'
}

interface SetReleaseAction {
	type: ActionType.SET_DEPLOYMENT;
	deployment: ExpandedDeployment | null;
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
	deployment: ExpandedDeployment | null;
	error: Error | null;
	loading: boolean;
}

export function initialState(): State {
	return {
		deployment: null,
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
			case ActionType.SET_DEPLOYMENT:
				nextState.deployment = action.deployment;
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

export default function useDeploymentWithDispatch(deploymentName: string, dispatch: Dispatcher) {
	const client = useClient();
	React.useEffect(() => {
		const cancel = client.streamDeployments(
			(res: StreamDeploymentsResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				const deployment = res.getDeploymentsList()[0] || null;
				if (!deployment) {
					dispatch([
						{ type: ActionType.SET_ERROR, error: new Error(`deployment (${deploymentName}) not found`) },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				dispatch([
					{ type: ActionType.SET_DEPLOYMENT, deployment },
					{ type: ActionType.SET_ERROR, error: null },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			},
			setNameFilters(deploymentName),
			setPageSize(1),
			setStreamCreates(),
			setStreamUpdates()
		);
		return cancel;
	}, [deploymentName, client, dispatch]);
}
