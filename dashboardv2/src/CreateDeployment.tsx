import * as React from 'react';
import { Checkmark as CheckmarkIcon } from 'grommet-icons';
import { Box, Button } from 'grommet';

import { Release, CreateScaleRequest } from './generated/controller_pb';
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
import {
	useReleaseWithDispatch,
	State as ReleaseState,
	initialState as initialReleaseState,
	reducer as releaseReducer,
	ActionType as ReleaseActionType,
	Action as ReleaseAction
} from './useRelease';
import isActionType from './util/isActionType';
import useWithCancel from './useWithCancel';
import Loading from './Loading';
import ReleaseComponent from './Release';
import ProcessesDiff, { ActionType as ProcessesDiffActionType, Action as ProcessesDiffAction } from './ProcessesDiff';

export enum ActionType {
	SET_CREATING = 'CreateDeployment__SET_CREATING',
	SET_ERROR = 'CreateDeployment__SET_ERROR',
	CANCEL = 'CreateDeployment__CANCEL',
	CREATED = 'CreateDeployment__CREATED'
}

interface SetCreatingAction {
	type: ActionType.SET_CREATING;
	creating: boolean;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error;
}

interface CancelAction {
	type: ActionType.CANCEL;
}

interface CreatedAction {
	type: ActionType.CREATED;
}

export type Action =
	| SetCreatingAction
	| SetErrorAction
	| AppReleaseAction
	| AppScaleAction
	| ReleaseAction
	| CancelAction
	| CreatedAction
	| ProcessesDiffAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	// useAppRelease
	currentReleaseState: AppReleaseState;

	// useAppScale
	currentScaleState: AppScaleState;

	// useRelease
	nextReleaseState: ReleaseState;

	isCreating: boolean;
	isScaleToZeroConfirmed: boolean;
}

function initialState(props: Props): State {
	return {
		// useAppRelease
		currentReleaseState: initialAppReleaseState(),

		// useAppScale
		currentScaleState: initialAppScaleState(),

		// useRelease
		nextReleaseState: initialReleaseState(),

		isCreating: false,
		isScaleToZeroConfirmed: props.newScale ? false : true
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	return actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
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

				// useRelease
				if (isActionType<ReleaseAction>(ReleaseActionType, action)) {
					nextState.nextReleaseState = releaseReducer(prevState.nextReleaseState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);
}

interface Props {
	appName: string;
	releaseName?: string;
	newRelease?: Release;
	newScale?: CreateScaleRequest;
	dispatch: Dispatcher;
}

export default function CreateDeployment(props: Props) {
	const client = useClient();
	const newRelease = props.newRelease;
	const newScale = props.newScale;
	const callerDispatch = props.dispatch;

	const [
		{
			// useAppRelease
			currentReleaseState: { release: currentRelease, loading: currentReleaseLoading, error: currentReleaseError },

			// useAppScale
			currentScaleState: { scale: currentScale, loading: currentScaleLoading, error: currentScaleError },

			// useRelease
			nextReleaseState: { release: nextRelease, loading: nextReleaseLoading, error: nextReleaseError },

			isCreating,
			isScaleToZeroConfirmed
		},
		localDispatch
	] = React.useReducer(reducer, initialState(props));
	const dispatch = useMergeDispatch(localDispatch, callerDispatch);

	useAppReleaseWithDispatch(props.appName, dispatch);
	useAppScaleWithDispatch(props.appName, dispatch);
	useReleaseWithDispatch(props.releaseName || '', dispatch);
	React.useEffect(() => {
		const error = currentReleaseError || nextReleaseError || currentScaleError;
		if (error) {
			dispatch({ type: ActionType.SET_ERROR, error });
		}
	}, [currentReleaseError, nextReleaseError, currentScaleError, dispatch]);
	const isLoading = React.useMemo(() => {
		return currentReleaseLoading || nextReleaseLoading || currentScaleLoading;
	}, [currentReleaseLoading, nextReleaseLoading, currentScaleLoading]);

	const withCancel = useWithCancel();

	function createRelease(newRelease: Release) {
		const { appName } = props;
		return new Promise((resolve, reject) => {
			const cancel = client.createRelease(appName, newRelease, (release: Release, error: Error | null) => {
				if (release && error === null) {
					resolve(release);
				} else {
					reject(error);
				}
			});
			withCancel.set(`createRelease(${appName})`, cancel);
		}) as Promise<Release>;
	}

	function createDeployment(release: Release, scale?: CreateScaleRequest) {
		let resolve: () => void, reject: (error: Error) => void;
		const p = new Promise((rs, rj) => {
			resolve = rs;
			reject = rj;
		});
		const cancel = client.createDeployment(release.getName(), scale || null, (error: Error | null) => {
			if (error) {
				reject(error);
			}
			resolve();
		});
		withCancel.set(`createDeployment(${release.getName()})`, cancel);
		return p;
	}

	function handleFormSubmit(e: React.SyntheticEvent) {
		e.preventDefault();
		const { newScale } = props;
		dispatch({ type: ActionType.SET_CREATING, creating: true });
		let p = Promise.resolve(null) as Promise<any>;
		if (newRelease) {
			p = createRelease(newRelease).then((release: Release) => {
				return createDeployment(release, newScale);
			});
		} else if (nextRelease) {
			p = createDeployment(nextRelease, newScale);
		}
		p.then(() => {
			queueMicrotask(() => {
				dispatch([{ type: ActionType.SET_CREATING, creating: false }, { type: ActionType.CREATED }]);
			});
		}).catch((error: Error) => {
			queueMicrotask(() => {
				dispatch({ type: ActionType.SET_ERROR, error });
			});
		});
	}

	if (isLoading) return <Loading />;

	if (!(nextRelease || newRelease)) {
		return null;
	}

	return (
		<Box tag="form" fill direction="column" onSubmit={handleFormSubmit} gap="small" justify="between">
			<Box>
				<h3>Review Changes</h3>
				<ReleaseComponent release={(nextRelease || newRelease) as Release} prevRelease={currentRelease} />

				{currentScale && newScale ? (
					<ProcessesDiff
						wrap
						align="center"
						direction="column"
						margin="small"
						scale={currentScale}
						nextScale={newScale}
						release={currentRelease}
						dispatch={dispatch}
						confirmScaleToZero
					/>
				) : null}
			</Box>

			<Box fill="horizontal" direction="row" align="end" gap="small" justify="between">
				<Button
					type="submit"
					disabled={isCreating || !isScaleToZeroConfirmed}
					primary
					icon={<CheckmarkIcon />}
					label={isCreating ? 'Deploying...' : 'Deploy'}
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
