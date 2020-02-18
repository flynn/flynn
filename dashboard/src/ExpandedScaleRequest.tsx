import * as React from 'react';
import { Grid, Box, Button, Text } from 'grommet';
import { ScaleRequest } from './generated/controller_pb';
import ProcessScale, { Action as ProcessScaleAction } from './ProcessScale';
import protoMapDiff, { DiffOp, DiffOption } from './util/protoMapDiff';
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
import Loading from './Loading';

export enum ActionType {
	// parent component should handle these actions
	DEPLOY_SCALE = 'ExpandedScaleRequest__DEPLOY_SCALE'
}

interface DeployScaleAction {
	type: ActionType.DEPLOY_SCALE;
	scale: ScaleRequest;
}

export type Action = DeployScaleAction | ProcessScaleAction | ScaleAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	// useScale START
	scaleState: ScaleState;
	// useScale END
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function initialState(): State {
	return {
		scaleState: initialScaleState()
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
				if (isActionType<ScaleAction>(ScaleActionType, action)) {
					nextState.scaleState = scaleReducer(prevState.scaleState, action);
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

export default function({ dispatch: callerDispatch }: Props) {
	const {
		urlParams,
		match: { params: matchParams },
		history
	} = useRouter();
	const [
		{
			scaleState: { scale, loading: scaleLoading }
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

	// TODO(jvatic): move this into reducer
	const diff = protoMapDiff(s.getOldProcessesMap(), s.getNewProcessesMap(), DiffOption.INCLUDE_UNCHANGED);
	const hasChanges = true; // TODO(jvatic): make this reflect the diff with the current app formation via useAppScale

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
