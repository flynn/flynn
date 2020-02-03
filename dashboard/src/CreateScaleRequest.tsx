import * as React from 'react';
import { Box, Button } from 'grommet';
import { Checkmark as CheckmarkIcon } from 'grommet-icons';

import useClient from './useClient';
import useMergeDispatch from './useMergeDispatch';
import {
	useAppReleaseWithDispatch,
	State as AppReleaseState,
	initialState as initialAppReleaseState,
	reducer as appReleaseReducer,
	ActionType as AppReleaseActionType,
	Action as AppReleaseAction
} from './useAppRelease';
import {
	useAppScaleWithDispatch,
	State as AppScaleState,
	initialState as initialAppScaleState,
	reducer as appScaleReducer,
	ActionType as AppScaleActionType,
	Action as AppScaleAction
} from './useAppScale';
import isActionType from './util/isActionType';
import useWithCancel from './useWithCancel';
import Loading from './Loading';
import ProcessesDiff, { ActionType as ProcessesDiffActionType, Action as ProcessesDiffAction } from './ProcessesDiff';
import protoMapDiff from './util/protoMapDiff';
import protoMapReplace from './util/protoMapReplace';
import buildProcessesMap from './util/buildProcessesMap';
import { ScaleRequest, CreateScaleRequest } from './generated/controller_pb';

export enum ActionType {
	SET_NEXT_SCALE = 'CreateScaleRequest__SET_NEXT_SCALE',
	SET_CREATING = 'CreateScaleRequest__SET_CREATING',
	SET_ERROR = 'CreateScaleRequest__SET_ERROR',
	SET_SCALE_TO_ZERO_CONFIRMED = 'CreateScaleRequest__SET_SCALE_TO_ZERO_CONFIRMED',
	CANCEL = 'CreateScaleRequest__CANCEL',
	CREATED = 'CreateScaleRequest__CREATED'
}

interface SetNextScaleAction {
	type: ActionType.SET_NEXT_SCALE;
	scale: CreateScaleRequest;
}

interface SetCreatingAction {
	type: ActionType.SET_CREATING;
	creating: boolean;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error;
}

interface SetScaleToZeroConfirmedAction {
	type: ActionType.SET_SCALE_TO_ZERO_CONFIRMED;
	confirmed: boolean;
}

interface CancelAction {
	type: ActionType.CANCEL;
}

interface CreatedAction {
	type: ActionType.CREATED;
	scale: ScaleRequest;
}

export type Action =
	| SetNextScaleAction
	| SetScaleToZeroConfirmedAction
	| SetCreatingAction
	| SetErrorAction
	| AppReleaseAction
	| AppScaleAction
	| CancelAction
	| CreatedAction
	| ProcessesDiffAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	// useAppRelease
	currentReleaseState: AppReleaseState;

	// useAppScale
	currentScaleState: AppScaleState;

	nextScale: CreateScaleRequest;
	isCreating: boolean;
	isScaleToZeroConfirmed: boolean;
	hasChanges: boolean;
}

function initialState(props: Props): State {
	return {
		// useAppRelease
		currentReleaseState: initialAppReleaseState(),

		// useAppScale
		currentScaleState: initialAppScaleState(),

		nextScale: new CreateScaleRequest(),
		isCreating: false,
		isScaleToZeroConfirmed: false,
		hasChanges: false
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_NEXT_SCALE:
				nextState.nextScale = action.scale;
				return nextState;

			case ActionType.SET_CREATING:
				nextState.isCreating = action.creating;
				return nextState;

			case ActionType.SET_ERROR:
				// no-op, parent component is expected to handle this
				return prevState;

			case ProcessesDiffActionType.SCALE_TO_ZERO_CONFIRMED:
				nextState.isScaleToZeroConfirmed = true;
				return nextState;

			case ProcessesDiffActionType.SCALE_TO_ZERO_UNCONFIRMED:
				nextState.isScaleToZeroConfirmed = false;
				return nextState;

			default:
				// useAppRelease
				if (isActionType<AppReleaseAction>(AppReleaseActionType, action)) {
					nextState.currentReleaseState = appReleaseReducer(prevState.currentReleaseState, action);
					return nextState;
				}

				// useAppScale
				if (isActionType<AppScaleAction>(AppScaleActionType, action)) {
					nextState.currentScaleState = appScaleReducer(prevState.currentScaleState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	// keep track of if selected scale actually changes anything
	(() => {
		const {
			currentScaleState: { scale },
			nextScale,
			currentReleaseState: { release }
		} = nextState;
		const {
			currentScaleState: { scale: prevScale },
			nextScale: prevNextScale,
			currentReleaseState: { release: prevRelease }
		} = prevState;
		if (scale === prevScale && nextScale === prevNextScale && release === prevRelease) return;
		const diff = protoMapDiff(
			buildProcessesMap((scale || new ScaleRequest()).getNewProcessesMap(), release),
			buildProcessesMap(nextScale.getProcessesMap(), release)
		);
		nextState.hasChanges = diff.length > 0;
	})();

	return nextState;
}

interface Props {
	appName: string;
	nextScale: CreateScaleRequest;
	dispatch: Dispatcher;
}

export default function CreateScaleRequestComponent(props: Props) {
	const { appName, nextScale, dispatch: callerDispatch } = props;
	const client = useClient();
	const withCancel = useWithCancel();

	const [
		{
			// useAppRelease
			currentReleaseState: { release, loading: releaseLoading, error: releaseError },

			// useAppScale
			currentScaleState: { scale, loading: scaleLoading, error: scaleError },

			isCreating,
			isScaleToZeroConfirmed,
			hasChanges
		},
		localDispatch
	] = React.useReducer(reducer, initialState(props));
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);

	// expose props.nextScale to state reducer
	React.useEffect(() => {
		dispatch({ type: ActionType.SET_NEXT_SCALE, scale: nextScale });
	}, [nextScale, dispatch]);

	useAppReleaseWithDispatch(appName, dispatch);
	useAppScaleWithDispatch(appName, dispatch);

	const isLoading = scaleLoading || releaseLoading;

	React.useEffect(() => {
		const error = scaleError || releaseError;
		if (error) {
			dispatch({ type: ActionType.SET_ERROR, error });
		}
	}, [scaleError, releaseError, dispatch]);

	function handleSubmit(e: React.SyntheticEvent) {
		e.preventDefault();

		dispatch({ type: ActionType.SET_CREATING, creating: true });

		const req = new CreateScaleRequest();
		req.setParent(nextScale.getParent() || (release ? release.getName() : ''));
		protoMapReplace(req.getProcessesMap(), nextScale.getProcessesMap());
		protoMapReplace(req.getTagsMap(), nextScale.getTagsMap());
		const cancel = client.createScale(req, (scaleReq: ScaleRequest, error: Error | null) => {
			if (error) {
				dispatch([
					{ type: ActionType.SET_ERROR, error },
					{ type: ActionType.SET_CREATING, creating: false }
				]);
				return;
			}
			dispatch([
				{ type: ActionType.SET_CREATING, creating: false },
				{ type: ActionType.CREATED, scale: scaleReq }
			]);
		});
		withCancel.set(`createScale(${req.getParent()})`, cancel);
	}

	if (isLoading) {
		return <Loading />;
	}

	if (!scale) throw new Error('<CreateScaleRequestComponent> Error: Unexpected lack of scale!');
	if (!release) throw new Error('<CreateScaleRequestComponent> Error: Unexpected lack of release!');

	return (
		<Box tag="form" fill direction="column" onSubmit={handleSubmit} gap="small" justify="between">
			<Box>
				<h3>Review Changes</h3>

				<ProcessesDiff
					wrap
					direction="row"
					margin="small"
					align="center"
					scale={scale}
					nextScale={nextScale}
					release={release}
					dispatch={dispatch}
				/>
			</Box>

			<Box fill="horizontal" direction="row" align="end" gap="small" justify="between">
				<Button
					type="submit"
					disabled={isCreating || !hasChanges || !isScaleToZeroConfirmed}
					primary
					icon={<CheckmarkIcon />}
					label={isCreating ? 'Scaling App...' : 'Scale App'}
				/>
				<Button
					type="button"
					label="Cancel"
					onClick={(e: React.SyntheticEvent) => {
						e.preventDefault();
						dispatch({ type: ActionType.CANCEL });
					}}
				/>
			</Box>
		</Box>
	);
}
