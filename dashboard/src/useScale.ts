import * as React from 'react';
import useClient from './useClient';
import { setNameFilters, setPageSize, setStreamCreates, setStreamUpdates } from './client';
import { ScaleRequest, ScaleRequestState, StreamScalesResponse } from './generated/controller_pb';

export enum ActionType {
	SET_SCALE = 'useScale__SET_SCALE',
	SET_ERROR = 'useScale__SET_ERROR',
	SET_LOADING = 'useScale__SET_LOADING'
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

export type Action = SetScaleAction | SetErrorAction | SetLoadingAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	scale: ScaleRequest | null;
	error: Error | null;
	loading: boolean;
}

export function initialState(): State {
	return {
		scale: null,
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
			case ActionType.SET_SCALE:
				nextState.scale = action.scale;
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

export default function useScaleWithDispatch(name: string, dispatch: Dispatcher) {
	const client = useClient();
	React.useEffect(() => {
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
			setNameFilters(name),
			setPageSize(1),
			setStreamCreates(),
			setStreamUpdates()
		);
		return cancel;
	}, [name, client, dispatch]);
}
