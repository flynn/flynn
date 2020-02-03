import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box, Button, Text } from 'grommet';

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
import useNavProtection from './useNavProtection';
import Loading from './Loading';
import RightOverlay from './RightOverlay';
import CreateScaleRequestComponent, {
	Action as CreateScaleRequestAction,
	ActionType as CreateScaleRequestActionType
} from './CreateScaleRequest';
import ProcessScale, {
	ActionType as ProcessScaleActionType,
	Action as ProcessScaleAction,
	Props as ProcessScaleProps
} from './ProcessScale';
import protoMapDiff, { applyProtoMapDiff, Diff } from './util/protoMapDiff';
import protoMapReplace from './util/protoMapReplace';
import buildProcessesMap from './util/buildProcessesMap';
import { ScaleRequest, ScaleRequestState, CreateScaleRequest } from './generated/controller_pb';

export enum ActionType {
	SET_PROCESSES = 'FormationEditor__SET_PROCESSES',
	PROCESS_CHANGE = 'FormationEditor__PROCESS_CHANGE',
	INCREMENT_PROCESS = 'FormationEditor__INCREMENT_PROCESS',
	DECREMENT_PROCESS = 'FormationEditor__DECREMENT_PROCESS',
	RESET = 'FormationEditor__RESET',
	SET_CONFIRMING = 'FormationEditor__SET_CONFIRMING',
	SET_ERROR = 'FormationEditor__SET_ERROR'
}

interface SetProcessesAction {
	type: ActionType.SET_PROCESSES;
	processes: [string, number][];
}

interface ProcessChangeAction {
	type: ActionType.PROCESS_CHANGE;
	key: string;
	val: number;
}

interface IncrementProcessAction {
	type: ActionType.INCREMENT_PROCESS;
	key: string;
}

interface DecrementProcessAction {
	type: ActionType.DECREMENT_PROCESS;
	key: string;
}

interface ResetAction {
	type: ActionType.RESET;
}

interface SetConfirmingAction {
	type: ActionType.SET_CONFIRMING;
	confirming: boolean;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error;
}

export type Action =
	| SetProcessesAction
	| ProcessChangeAction
	| IncrementProcessAction
	| DecrementProcessAction
	| ResetAction
	| SetConfirmingAction
	| SetErrorAction
	| AppReleaseAction
	| AppScaleAction
	| CreateScaleRequestAction
	| ProcessScaleAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	// useAppRelease
	releaseState: AppReleaseState;

	// useAppScale
	scaleState: AppScaleState;

	initialProcesses: jspb.Map<string, number>;
	processes: [string, number][];
	processesDiff: Diff<string, number>;
	hasChanges: boolean;
	isConfirming: boolean;
	nextScale: CreateScaleRequest;
}

function initialState(props: Props): State {
	return {
		// useAppRelease
		releaseState: initialAppReleaseState(),

		// useAppScale
		scaleState: initialAppScaleState(),

		initialProcesses: new jspb.Map<string, number>([]),
		processes: [],
		processesDiff: [],
		hasChanges: false,
		isConfirming: false,
		nextScale: new CreateScaleRequest()
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

function buildProcessesArray(m: jspb.Map<string, number>): [string, number][] {
	return Array.from(m.getEntryList()).sort(([ak, av]: [string, number], [bk, bv]: [string, number]) => {
		return ak.localeCompare(bk);
	});
}

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_PROCESSES:
				nextState.processes = action.processes;
				return nextState;

			case ActionType.PROCESS_CHANGE:
				nextState.processes = prevState.processes.map(([k, v]: [string, number]) => {
					if (k === action.key) {
						return [k, action.val];
					}
					return [k, v];
				}) as [string, number][];

				return nextState;

			case ActionType.INCREMENT_PROCESS:
				nextState.processes = prevState.processes.map(([k, v]: [string, number]) => {
					if (k === action.key) {
						return [k, v + 1];
					}
					return [k, v];
				}) as [string, number][];

				return nextState;

			case ActionType.DECREMENT_PROCESS:
				let changed = false;
				nextState.processes = prevState.processes.map(([k, v]: [string, number]) => {
					if (k === action.key) {
						const nextValue = Math.max(v - 1, 0);
						if (nextValue !== v) {
							changed = true;
						}
						return [k, nextValue];
					}
					return [k, v];
				}) as [string, number][];

				if (!changed) return prevState;

				return nextState;

			case ActionType.RESET:
				nextState.processes = buildProcessesArray(prevState.initialProcesses);
				return nextState;

			case ActionType.SET_CONFIRMING:
				nextState.isConfirming = action.confirming;
				return nextState;

			case ActionType.SET_ERROR:
				// no-op, parent component is expected to handle this
				// see <AppComponent>
				return prevState;

			case CreateScaleRequestActionType.CREATED:
				return reducer(prevState, { type: ActionType.SET_CONFIRMING, confirming: false });

			case CreateScaleRequestActionType.CANCEL:
				return reducer(prevState, { type: ActionType.SET_CONFIRMING, confirming: false });

			default:
				// useAppRelease
				if (isActionType<AppReleaseAction>(AppReleaseActionType, action)) {
					nextState.releaseState = appReleaseReducer(prevState.releaseState, action);
					return nextState;
				}

				// useAppScale
				if (isActionType<AppScaleAction>(AppScaleActionType, action)) {
					nextState.scaleState = appScaleReducer(prevState.scaleState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	// preserve changes
	(() => {
		const {
			scaleState: { scale },
			releaseState: { release },
			hasChanges,
			processesDiff
		} = nextState;
		const {
			scaleState: { scale: prevScale },
			releaseState: { release: prevRelease }
		} = prevState;
		if (scale === prevScale && release === prevRelease) return;
		if (!scale || !release) return;

		let processesMap = scale.getNewProcessesMap();
		if (hasChanges) {
			processesMap = applyProtoMapDiff(processesMap, processesDiff);
		}

		nextState.processes = buildProcessesArray(buildProcessesMap(processesMap, release));
		nextState.initialProcesses = buildProcessesMap(scale.getNewProcessesMap(), release);
	})();

	// set `processesDiff` and `hasChanges` when `processes` changes
	(() => {
		const { initialProcesses, processes } = nextState;

		const { processes: prevProcesses } = prevState;
		if (processes === prevProcesses) return;

		const diff = protoMapDiff(initialProcesses, new jspb.Map(processes));
		nextState.processesDiff = diff;
		nextState.hasChanges = diff.length > 0;
	})();

	// used to render diff
	(() => {
		const {
			scaleState: { scale },
			processes
		} = nextState;

		const {
			scaleState: { scale: prevScale },
			processes: prevProcesses
		} = prevState;
		if (scale === prevScale && processes === prevProcesses) return;

		const s = new CreateScaleRequest();
		if (scale) {
			s.setParent(scale.getParent());
			protoMapReplace(s.getTagsMap(), scale.getNewTagsMap());
		}
		protoMapReplace(s.getProcessesMap(), new jspb.Map(processes));
		nextState.nextScale = s;
	})();

	return nextState;
}

interface Props {
	appName: string;
	dispatch: Dispatcher;
}

export default function FormationEditor(props: Props) {
	const { appName, dispatch: callerDispatch } = props;
	const client = useClient();
	const withCancel = useWithCancel();

	const [
		{
			// useAppRelease
			releaseState: { release, loading: releaseLoading, error: releaseError },

			// useAppScale
			scaleState: { scale, loading: scaleLoading, error: scaleError },

			processes,
			hasChanges,
			isConfirming,
			nextScale
		},
		localDispatch
	] = React.useReducer(reducer, initialState(props));
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);
	useAppScaleWithDispatch(appName, dispatch);
	useAppReleaseWithDispatch(appName, dispatch);

	const isLoading = scaleLoading || releaseLoading;

	React.useEffect(() => {
		const error = scaleError || releaseError;
		if (error) {
			dispatch({ type: ActionType.SET_ERROR, error });
		}
	}, [scaleError, releaseError, dispatch]);

	const [enableNavProtection, disableNavProtection] = useNavProtection();
	React.useEffect(
		() => {
			if (hasChanges) {
				enableNavProtection();
			} else {
				disableNavProtection();
			}
		},
		[hasChanges] // eslint-disable-line react-hooks/exhaustive-deps
	);

	function handleSubmit(e: React.SyntheticEvent) {
		e.preventDefault();
		dispatch({ type: ActionType.SET_CONFIRMING, confirming: true });
	}

	function handleConfirmSubmit(e: React.SyntheticEvent) {
		e.preventDefault();

		// build new formation object with new processes map
		if (!scale) return; // should never be null at this point
		if (!release) return; // should never be null at this point

		dispatch({ type: ActionType.SET_CONFIRMING, confirming: false });

		const req = new CreateScaleRequest();
		req.setParent(scale.getParent());
		protoMapReplace(req.getProcessesMap(), new jspb.Map(processes));
		protoMapReplace(req.getTagsMap(), scale.getNewTagsMap());
		const cancel = client.createScale(req, (scaleReq: ScaleRequest, error: Error | null) => {
			if (error) {
				dispatch({ type: ActionType.SET_ERROR, error });
				return;
			}
			dispatch({
				type: ActionType.SET_PROCESSES,
				processes: buildProcessesArray(buildProcessesMap(scaleReq.getNewProcessesMap(), release))
			});
		});
		withCancel.set(`createScale(${req.getParent()})`, cancel);
	}

	const handleScaleCancel = (e?: React.SyntheticEvent) => {
		e ? e.preventDefault() : void 0;
		dispatch({ type: ActionType.SET_CONFIRMING, confirming: false });
	};

	if (isLoading) {
		return <Loading />;
	}

	if (!scale) throw new Error('<FormationEditor> Error: Unexpected lack of scale!');
	if (!release) throw new Error('<FormationEditor> Error: Unexpected lack of release!');

	const isPending = scale.getState() === ScaleRequestState.SCALE_PENDING;

	return (
		<>
			{isConfirming ? (
				<RightOverlay onClose={handleScaleCancel}>
					<CreateScaleRequestComponent appName={appName} nextScale={nextScale} dispatch={dispatch} />
				</RightOverlay>
			) : null}

			<Box as="form" onSubmit={isConfirming ? handleConfirmSubmit : handleSubmit} margin={{ bottom: 'xsmall' }}>
				<Box wrap direction="row" gap="small" margin={{ bottom: 'xsmall' }}>
					{processes.length === 0 ? (
						<Text color="dark-2">&lt;No processes&gt;</Text>
					) : (
						processes.map(([key, val]: [string, number]) => (
							<Box margin={{ bottom: 'xsmall' }} align="center" key={key}>
								<WrappedProcessScale processesKey={key} value={val} label={key} mutable dispatch={dispatch} />
							</Box>
						))
					)}
				</Box>
				<Box direction="row">
					{hasChanges && !isPending ? (
						<>
							<Button type="submit" primary={true} label="Scale App" />
							&nbsp;
							<Button
								type="button"
								label="Reset"
								onClick={(e: React.SyntheticEvent) => {
									e.preventDefault();
									dispatch({ type: ActionType.RESET });
								}}
							/>
						</>
					) : (
						<Button disabled type="button" primary={true} label="Scale App" />
					)}
				</Box>
			</Box>
		</>
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
					case ProcessScaleActionType.SET_VALUE:
						parentDispatch({
							type: ActionType.PROCESS_CHANGE,
							key,
							val: action.value
						});
						break;
					case ProcessScaleActionType.INCREMENT_VALUE:
						parentDispatch({
							type: ActionType.INCREMENT_PROCESS,
							key
						});
						break;
					case ProcessScaleActionType.DECREMENT_VALUE:
						parentDispatch({
							type: ActionType.DECREMENT_PROCESS,
							key
						});
						break;
				}
			});
		},
		[key, parentDispatch]
	);
	return <ProcessScale dispatch={dispatch} {...props} />;
};
