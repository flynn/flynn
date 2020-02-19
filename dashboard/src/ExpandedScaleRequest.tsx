import * as React from 'react';
import { Grid, Box, Text } from 'grommet';
import Button from './Button';
import { ScaleRequest } from './generated/controller_pb';
import ProcessScale, { Action as ProcessScaleAction } from './ProcessScale';
import protoMapDiff, { Diff, DiffOp, DiffOption } from './util/protoMapDiff';
import useRouter from './useRouter';
import isActionType from './util/isActionType';
import useMergeDispatch from './useMergeDispatch';
import useScaleWithDispatch, {
	ActionType as ScaleActionType,
	Action as ScaleAction,
	State as ScaleState,
	reducer as scaleReducer,
	initialState as initialScaleState
} from './useScale';
import {
	useAppScaleWithDispatch,
	Action as AppScaleAction,
	ActionType as AppScaleActionType,
	State as AppScaleState,
	reducer as appScaleReducer,
	initialState as initialAppScaleState
} from './useAppScale';
import Loading from './Loading';

export enum ActionType {
	// parent component should handle these actions
	DEPLOY_SCALE = 'ExpandedScaleRequest__DEPLOY_SCALE'
}

interface DeployScaleAction {
	type: ActionType.DEPLOY_SCALE;
	scale: ScaleRequest;
}

export type Action = DeployScaleAction | ProcessScaleAction | ScaleAction | AppScaleAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	// useScale
	scaleState: ScaleState;

	// useAppScale
	currentScaleState: AppScaleState;

	// true if scaleState.scale different from currentScaleState.scale
	hasChanges: boolean;

	diff: Diff<string, number>;
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function initialState(): State {
	return {
		// useScale
		scaleState: initialScaleState(),

		// useAppScale
		currentScaleState: initialAppScaleState(),

		hasChanges: false,

		diff: []
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
				// useScale
				if (isActionType<ScaleAction>(ScaleActionType, action)) {
					nextState.scaleState = scaleReducer(prevState.scaleState, action);
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

	(() => {
		const {
			currentScaleState: { scale: currentScale },
			scaleState: { scale: nextScale }
		} = nextState;
		if (prevState.currentScaleState.scale === currentScale && prevState.scaleState.scale === nextScale) {
			return;
		}
		if (!currentScale || !nextScale) return;
		const diff = protoMapDiff(currentScale.getNewProcessesMap(), nextScale.getNewProcessesMap());
		nextState.hasChanges = diff.length > 0;
	})();

	(() => {
		const {
			scaleState: { scale: s }
		} = nextState;
		if (prevState.scaleState.scale === s) {
			return;
		}
		if (!s) return;
		nextState.diff = protoMapDiff(s.getOldProcessesMap(), s.getNewProcessesMap(), DiffOption.INCLUDE_UNCHANGED);
	})();

	return nextState;
}

interface Props {
	dispatch: Dispatcher;
}

export default function({ dispatch: callerDispatch }: Props) {
	const {
		urlParams,
		match: { params: matchParams },
		history
	} = useRouter();
	const [
		{
			scaleState: { scale, loading: scaleLoading },
			hasChanges,
			diff
		},
		localDispatch
	] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);
	const s = scale || new ScaleRequest();

	const { appID, releaseID, scaleRequestID } = matchParams;
	const appName = `apps/${appID}`;
	const releaseName = `${appName}/releases/${releaseID}`;
	const scaleRequestName = `${releaseName}/scales/${scaleRequestID}`;
	useScaleWithDispatch(scaleRequestName, dispatch);
	useAppScaleWithDispatch(appName, dispatch);

	const handleSubmit = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			dispatch({ type: ActionType.DEPLOY_SCALE, scale: s });
		},
		[s, dispatch]
	);

	const handleCloseBtnClick = React.useCallback(
		(e: React.SyntheticEvent) => {
			e.preventDefault();
			history.push({ pathname: `/${appName}`, search: urlParams.toString() });
		},
		[appName, urlParams, history]
	);

	if (scaleLoading) return <Loading />;

	return (
		<Box tag="form" fill direction="column" onSubmit={handleSubmit} gap="small" justify="between">
			<Box>
				<h3>Release {releaseID}</h3>
				<h3>Scale {scaleRequestID}</h3>
				<Grid justify="start" columns="small">
					{diff.length === 0 ? <Text color="dark-2">&lt;No processes&gt;</Text> : null}
					{diff.reduce((m: React.ReactNodeArray, op: DiffOp<string, number>) => {
						if (op.op === 'remove') {
							return m;
						}
						let val = op.value;
						let prevVal = s.getOldProcessesMap().get(op.key);
						if (op.op === 'keep') {
							val = prevVal;
						}
						m.push(
							<ProcessScale
								key={op.key}
								margin="xsmall"
								size="xsmall"
								value={val as number}
								originalValue={prevVal}
								showDelta
								label={op.key}
								dispatch={dispatch}
							/>
						);
						return m;
					}, [] as React.ReactNodeArray)}
				</Grid>
			</Box>
			<Box fill="horizontal" direction="row" align="end" gap="small" justify="between">
				<Button type="submit" disabled={!hasChanges} primary label="Rollback to process config" />
				<Button type="button" label="Close" onClick={handleCloseBtnClick} />
			</Box>
		</Box>
	);
}
