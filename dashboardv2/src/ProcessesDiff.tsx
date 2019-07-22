import * as React from 'react';
import { Box, BoxProps } from 'grommet';

import ProcessScale, {
	ActionType as ProcessScaleActionType,
	Action as ProcessScaleAction,
	Props as ProcessScaleProps
} from './ProcessScale';
import protoMapDiff, { Diff, DiffOp, DiffOption } from './util/protoMapDiff';
import buildProcessesMap from './util/buildProcessesMap';
import { ScaleRequest, CreateScaleRequest, ScaleRequestState, Release } from './generated/controller_pb';
import useMergeDispatch from './useMergeDispatch';

export enum ActionType {
	SET_SCALE_TO_ZERO_CONFIRMED = 'ProcessesDiff__SET_SCALE_TO_ZERO_CONFIRMED',
	SCALE_TO_ZERO_CONFIRMED = 'ProcessesDiff__SCALE_TO_ZERO_CONFIRMED',
	SCALE_TO_ZERO_UNCONFIRMED = 'ProcessesDiff__SCALE_TO_ZERO_UNCONFIRMED',
	PROPS_UPDATED = 'ProcessesDiff__PROPS_UPDATED'
}

interface SetScaleToZeroConfirmedAction {
	type: ActionType.SET_SCALE_TO_ZERO_CONFIRMED;
	key: string;
	confirmed: boolean;
}

interface PropsUpdatedAction {
	type: ActionType.PROPS_UPDATED;
	props: StateProps;
}

interface ScaleToZeroConfirmedAction {
	type: ActionType.SCALE_TO_ZERO_CONFIRMED;
}

interface ScaleToZeroUnconfirmedAction {
	type: ActionType.SCALE_TO_ZERO_UNCONFIRMED;
}

export type Action =
	| SetScaleToZeroConfirmedAction
	| PropsUpdatedAction
	| ScaleToZeroConfirmedAction
	| ScaleToZeroUnconfirmedAction
	| ProcessScaleAction;

type Dispatcher = (actions: Action | Action[]) => void;

function buildProcessesFullDiff(
	scale: ScaleRequest,
	nextScale: CreateScaleRequest,
	release: Release | null
): Diff<string, number> {
	return protoMapDiff(
		buildProcessesMap((scale || new ScaleRequest()).getNewProcessesMap(), release),
		buildProcessesMap(nextScale.getProcessesMap(), release),
		DiffOption.INCLUDE_UNCHANGED,
		DiffOption.NO_DUPLICATE_KEYS
	);
}

function buildScaleToZeroConfirmationRequired(processesFullDiff: Diff<string, number>): Set<string> {
	const keys = new Set<string>();
	processesFullDiff.forEach((op) => {
		if (op.op === 'remove' || op.value === 0) {
			keys.add(op.key);
		}
	});
	return keys;
}

function buildScaleToZeroConfirmed(): Map<string, boolean> {
	return new Map<string, boolean>();
}

function buildIsScaleToZeroConfirmed(
	scaleToZeroConfirmationRequired: Set<string>,
	scaleToZeroConfirmed: Map<string, boolean>
): boolean {
	let isConfirmed = true;
	for (let k of scaleToZeroConfirmationRequired) {
		if (scaleToZeroConfirmed.get(k) !== true) {
			isConfirmed = false;
		}
	}
	return isConfirmed;
}

interface State extends StateProps {
	scaleToZeroConfirmationRequired: Set<string>;
	isScaleToZeroConfirmed: boolean;
	scaleToZeroConfirmed: Map<string, boolean>;
	processesFullDiff: Diff<string, number>;
}

function initialState({ scale, nextScale, release, confirmScaleToZero = true }: StateProps): State {
	const processesFullDiff = buildProcessesFullDiff(scale, nextScale, release);
	const scaleToZeroConfirmationRequired = buildScaleToZeroConfirmationRequired(processesFullDiff);
	const scaleToZeroConfirmed = buildScaleToZeroConfirmed();
	const isScaleToZeroConfirmed = buildIsScaleToZeroConfirmed(scaleToZeroConfirmationRequired, scaleToZeroConfirmed);
	return {
		scaleToZeroConfirmationRequired,
		isScaleToZeroConfirmed,
		scaleToZeroConfirmed,
		processesFullDiff,

		scale,
		nextScale,
		release,
		confirmScaleToZero
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
			case ActionType.SET_SCALE_TO_ZERO_CONFIRMED:
				const nextMap = new Map(nextState.scaleToZeroConfirmed);
				nextMap.set(action.key, action.confirmed);
				nextState.scaleToZeroConfirmed = nextMap;
				return nextState;

			case ActionType.PROPS_UPDATED:
				Object.assign(nextState, action.props);
				return nextState;

			default:
				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		const { scale, nextScale, release } = nextState;
		if (scale === prevState.scale && nextScale === prevState.nextScale && release === prevState.release) return;

		nextState.processesFullDiff = buildProcessesFullDiff(scale, nextScale, release);
	})();

	(() => {
		const { confirmScaleToZero, processesFullDiff } = nextState;
		if (confirmScaleToZero === prevState.confirmScaleToZero && processesFullDiff === prevState.processesFullDiff)
			return;
		if (!confirmScaleToZero) return;

		nextState.scaleToZeroConfirmationRequired = buildScaleToZeroConfirmationRequired(processesFullDiff);
	})();

	(() => {
		const { scaleToZeroConfirmationRequired, scaleToZeroConfirmed } = nextState;
		if (
			scaleToZeroConfirmationRequired === prevState.scaleToZeroConfirmationRequired &&
			scaleToZeroConfirmed === prevState.scaleToZeroConfirmed
		)
			return;

		nextState.isScaleToZeroConfirmed = buildIsScaleToZeroConfirmed(
			scaleToZeroConfirmationRequired,
			scaleToZeroConfirmed
		);
	})();

	return nextState;
}

interface StateProps {
	scale: ScaleRequest;
	nextScale: CreateScaleRequest;
	release: Release | null;
	confirmScaleToZero: boolean;
}

interface Props extends Pick<StateProps, Exclude<keyof StateProps, 'confirmScaleToZero'>>, BoxProps {
	confirmScaleToZero?: boolean;
	dispatch: Dispatcher;
}

export default function ProcessesDiff({
	scale,
	nextScale,
	release = null,
	confirmScaleToZero = true,
	dispatch: callerDispatch,
	direction,
	...boxProps
}: Props) {
	const [{ isScaleToZeroConfirmed, scaleToZeroConfirmed, processesFullDiff }, localDispatch] = React.useReducer(
		reducer,
		initialState({ scale, nextScale, release, confirmScaleToZero })
	);
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);

	React.useEffect(() => {
		dispatch({ type: ActionType.PROPS_UPDATED, props: { scale, nextScale, release, confirmScaleToZero } });
	}, [scale, nextScale, release, confirmScaleToZero, dispatch]);

	React.useEffect(() => {
		if (isScaleToZeroConfirmed) {
			dispatch({ type: ActionType.SCALE_TO_ZERO_CONFIRMED });
		} else {
			dispatch({ type: ActionType.SCALE_TO_ZERO_UNCONFIRMED });
		}
	}, [isScaleToZeroConfirmed, dispatch]);

	const isPending = scale.getState() === ScaleRequestState.SCALE_PENDING;

	return (
		<Box direction="row" gap="small" {...boxProps}>
			{processesFullDiff.reduce((m: React.ReactNodeArray, op: DiffOp<string, number>) => {
				const key = op.key;
				let startVal = scale.getNewProcessesMap().get(key) || 0;
				let val = op.value || 0;
				if (op.op === 'remove') {
					val = 0;
				}
				if (op.op === 'keep') {
					val = startVal;
				}
				m.push(
					<Box align="center" key={key}>
						<WrappedProcessScale
							processesKey={key}
							direction={direction}
							confirmScaleToZero={confirmScaleToZero}
							scaleToZeroConfirmed={scaleToZeroConfirmed.get(key)}
							value={val}
							originalValue={startVal}
							showLabelDelta={!isPending}
							label={key}
							dispatch={dispatch}
						/>
					</Box>
				);
				return m;
			}, [] as React.ReactNodeArray)}
		</Box>
	);
}

interface WrappedProcessScaleProps extends ProcessScaleProps {
	processesKey: string;
	dispatch: Dispatcher;
}

const WrappedProcessScale = ({ processesKey: key, dispatch: parentDispatch, ...props }: WrappedProcessScaleProps) => {
	const dispatch = React.useCallback(
		(actions: Action | Action[]) => {
			if (!Array.isArray(actions)) actions = [actions];
			actions.forEach((action: Action) => {
				switch (action.type) {
					case ProcessScaleActionType.CONFIRM_SCALE_TO_ZERO:
						parentDispatch({ type: ActionType.SET_SCALE_TO_ZERO_CONFIRMED, key, confirmed: true });
						return;

					case ProcessScaleActionType.UNCONFIRM_SCALE_TO_ZERO:
						parentDispatch({ type: ActionType.SET_SCALE_TO_ZERO_CONFIRMED, key, confirmed: false });
						return;
				}
			});
		},
		[key, parentDispatch]
	);
	return <ProcessScale dispatch={dispatch} {...props} />;
};
