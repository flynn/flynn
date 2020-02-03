import * as React from 'react';
import useClient from './useClient';
import { setNameFilters, setPageSize, setStreamUpdates } from './client';
import { App, StreamAppsResponse } from './generated/controller_pb';

export enum ActionType {
	SET_APP = 'useApp__SET_APP',
	SET_ERROR = 'useApp__SET_ERROR',
	SET_LOADING = 'useApp__SET_LOADING'
}

interface SetAppAction {
	type: ActionType.SET_APP;
	app: App | null;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error | null;
}

interface SetLoadingAction {
	type: ActionType.SET_LOADING;
	loading: boolean;
}

export type Action = SetAppAction | SetErrorAction | SetLoadingAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	app: App | null;
	loading: boolean;
	error: Error | null;
}

export function initialState(): State {
	return {
		app: null,
		loading: true,
		error: null
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
			case ActionType.SET_APP:
				if (action.app) {
					nextState.app = action.app;
					return nextState;
				}
				return prevState;

			case ActionType.SET_LOADING:
				nextState.loading = action.loading;
				return nextState;

			case ActionType.SET_ERROR:
				nextState.error = action.error;
				return nextState;

			default:
				return prevState;
		}
	}, prevState);
}

export function useAppWithDispatch(appName: string, dispatch: Dispatcher) {
	const client = useClient();
	React.useEffect(() => {
		// no-op if called with empty appName
		if (appName === '') return () => {};

		const cancel = client.streamApps(
			(res: StreamAppsResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				const app = res.getAppsList()[0] || null;
				if (!app) {
					error = new Error('App not found');
				}
				dispatch([
					{ type: ActionType.SET_APP, app },
					{ type: ActionType.SET_ERROR, error },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			},
			setNameFilters(appName),
			setPageSize(1),
			setStreamUpdates()
		);
		return cancel;
	}, [appName, client, dispatch]);
}

export default function useApp(appName: string) {
	const [{ app, loading, error }, dispatch] = React.useReducer(reducer, initialState());
	useAppWithDispatch(appName, dispatch);
	return {
		app,
		loading,
		error
	};
}
