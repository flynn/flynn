import * as React from 'react';
import useClient from './useClient';
import useMergeDispatch from './useMergeDispatch';
import { useAppWithDispatch, Action as AppAction, ActionType as AppActionType } from './useApp';
import { setNameFilters, filterScalesByState, setPageSize, setStreamCreates, setStreamUpdates } from './client';
import { ScaleRequest, ScaleRequestState, StreamScalesResponse } from './generated/controller_pb';

export enum ActionType {
	SET_SCALE = 'useAppScale__SET_SCALE',
	SET_ERROR = 'useAppScale__SET_ERROR',
	SET_LOADING = 'useAppScale__SET_LOADING'
}

interface SetScaleAction {
	type: ActionType.SET_SCALE;
	scale: ScaleRequest | null;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error | null;
}

interface SetLoadingAction {
	type: ActionType.SET_LOADING;
	loading: boolean;
}

export type Action = SetScaleAction | SetErrorAction | SetLoadingAction | AppAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	scale: ScaleRequest | null;
	error: Error | null;
	loading: boolean;

	releaseName: string;
	appLoading: boolean;
	appError: Error | null;
}

export function initialState(): State {
	return {
		scale: null,
		error: null,
		loading: true,

		releaseName: '',
		appLoading: true,
		appError: null
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
			case ActionType.SET_SCALE:
				nextState.scale = action.scale;
				return nextState;

			case ActionType.SET_ERROR:
				nextState.error = action.error;
				return nextState;

			case ActionType.SET_LOADING:
				nextState.loading = action.loading;
				return nextState;

			// useApp START
			case AppActionType.SET_APP:
				if (action.app) {
					nextState.releaseName = action.app.getRelease();
					return nextState;
				}
				return prevState;

			case AppActionType.SET_LOADING:
				nextState.appLoading = action.loading;
				return nextState;

			case AppActionType.SET_ERROR:
				nextState.appError = action.error;
				return nextState;
			// useApp END

			default:
				return prevState;
		}
	}, prevState);
}

export function useAppScaleWithDispatch(appName: string, callerDispatch: Dispatcher) {
	const client = useClient();
	const [{ releaseName }, localDispatch] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);
	useAppWithDispatch(appName, dispatch);
	React.useEffect(() => {
		if (!releaseName) {
			const scale = new ScaleRequest();
			scale.setState(ScaleRequestState.SCALE_COMPLETE);
			dispatch([
				{ type: ActionType.SET_SCALE, scale },
				{ type: ActionType.SET_LOADING, loading: false }
			]);
			return;
		}
		const cancel = client.streamScales(
			(res: StreamScalesResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				const scales = res.getScaleRequestsList();
				let scale;
				if (scales.length === 0) {
					scale = new ScaleRequest();
					scale.setState(ScaleRequestState.SCALE_COMPLETE);
				} else {
					scale = scales[0];
				}
				dispatch([
					{ type: ActionType.SET_SCALE, scale },
					{ type: ActionType.SET_ERROR, error: null },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			},
			setNameFilters(releaseName),
			filterScalesByState(ScaleRequestState.SCALE_COMPLETE),
			setPageSize(1),
			setStreamCreates(),
			setStreamUpdates()
		);
		return cancel;
	}, [releaseName, client, dispatch]);
}

export default function useAppScale(appName: string) {
	const [{ scale, loading, error, appLoading, appError }, localDispatch] = React.useReducer(reducer, initialState());
	useAppScaleWithDispatch(appName, localDispatch);
	return {
		loading: appLoading || loading,
		scale,
		error: appError || error
	};
}
