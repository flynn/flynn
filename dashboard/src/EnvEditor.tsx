import * as React from 'react';
import * as jspb from 'google-protobuf';
import Loading from './Loading';
import CreateDeployment, {
	Action as CreateDeploymentAction,
	ActionType as CreateDeploymentActionType
} from './CreateDeployment';
import KeyValueEditor, {
	State as KVState,
	initialState as initialKVState,
	buildState as buildKVState,
	reducer as kvReducer,
	DataActionType,
	Action as KVEditorAction,
	ActionType as KVEditorActionType,
	isEditorAction as isKVActionType,
	getEntries
} from './KeyValueEditor';
import protoMapDiff, { applyProtoMapDiff } from './util/protoMapDiff';
import protoMapReplace from './util/protoMapReplace';
import isActionType from './util/isActionType';
import useErrorHandler from './useErrorHandler';
import { Release } from './generated/controller_pb';
import RightOverlay from './RightOverlay';
import { isNotFoundError } from './client';
import {
	useAppReleaseWithDispatch,
	ActionType as AppReleaseActionType,
	Action as AppReleaseAction,
	State as AppReleaseState,
	initialState as initialAppReleaseState,
	reducer as appReleaseReducer
} from './useAppRelease';
import useNavProtection from './useNavProtection';

interface Props {
	appName: string;
}

interface State {
	kvState: KVState;
	isDeploying: boolean;
	currentReleaseState: AppReleaseState;
	newRelease: Release;
}

function initialState(props: Props): State {
	return {
		kvState: initialKVState({}),
		isDeploying: false,
		currentReleaseState: initialAppReleaseState(),
		newRelease: new Release()
	};
}

enum ActionType {
	DEPLOY_DISMISS = 'DEPLOY_DISMISS'
}

interface DeployDismissAction {
	type: ActionType.DEPLOY_DISMISS;
}

type Action = DeployDismissAction | KVEditorAction | CreateDeploymentAction | AppReleaseAction;

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case KVEditorActionType.SUBMIT_DATA:
				nextState.kvState = kvReducer(
					buildKVState(prevState.kvState, Object.assign({}, prevState.kvState, { data: action.data })),
					action
				);
				nextState.isDeploying = nextState.kvState.data.hasChanges;
				return nextState;

			case CreateDeploymentActionType.CANCEL:
			case ActionType.DEPLOY_DISMISS:
				nextState.isDeploying = false;
				return nextState;

			case CreateDeploymentActionType.CREATED:
				nextState.isDeploying = false;
				return nextState;

			default:
				if (isKVActionType(action)) {
					nextState.kvState = kvReducer(prevState.kvState, action);
					return nextState;
				}

				if (isActionType<AppReleaseAction>(AppReleaseActionType, action)) {
					nextState.currentReleaseState = appReleaseReducer(prevState.currentReleaseState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	// handle app not having a release
	(() => {
		const {
			currentReleaseState: { release, loading, error }
		} = nextState;
		const {
			currentReleaseState: { release: prevRelease, loading: prevLoading, error: prevError }
		} = prevState;
		if (release === prevRelease && loading === prevLoading && error === prevError) return;
		if (!release && !loading) {
			nextState.currentReleaseState.release = new Release();
		}
	})();

	// newRelease is used to create a deployment
	(() => {
		const {
			currentReleaseState: { release },
			kvState: { data }
		} = nextState;
		if (!release) return;
		if (release === prevState.currentReleaseState.release && data === prevState.kvState.data) return;

		const diff = data ? protoMapDiff(release.getEnvMap(), new jspb.Map(getEntries(data))) : [];
		const newRelease = new Release();
		newRelease.setArtifactsList(release.getArtifactsList());
		protoMapReplace(newRelease.getLabelsMap(), release.getLabelsMap());
		protoMapReplace(newRelease.getProcessesMap(), release.getProcessesMap());
		protoMapReplace(newRelease.getEnvMap(), applyProtoMapDiff(release.getEnvMap(), diff));
		nextState.newRelease = newRelease;
	})();

	// maintain any non-conflicting changes made when new release arrives
	(() => {
		const {
			currentReleaseState: { release }
		} = nextState;
		if (!release) return;
		if (release === prevState.currentReleaseState.release) return;
		nextState.kvState = kvReducer(prevState.kvState, {
			type: DataActionType.REBASE,
			base: release.getEnvMap().toArray()
		});
	})();

	return nextState;
}

export default function EnvEditor(props: Props) {
	const { appName } = props;

	const [
		{
			kvState,
			isDeploying,
			currentReleaseState: { release, loading: releaseIsLoading, error: releaseError },
			newRelease
		},
		dispatch
	] = React.useReducer(reducer, initialState(props));
	const { data } = kvState;
	// Stream app release
	useAppReleaseWithDispatch(appName, dispatch);

	const [enableNavProtection, disableNavProtection] = useNavProtection();
	React.useEffect(() => {
		if (data && data.hasChanges) {
			enableNavProtection();
		} else {
			disableNavProtection();
		}
	}, [data, disableNavProtection, enableNavProtection]);

	const handleError = useErrorHandler();
	React.useEffect(() => {
		// handle any non-404 errors (not all apps have a release yet)
		let cancel = () => {};
		if (releaseError && !isNotFoundError(releaseError)) {
			cancel = handleError(releaseError);
		}
		return cancel;
	}, [handleError, releaseError]);

	const handleDeployDismiss = React.useCallback(() => {
		dispatch({ type: ActionType.DEPLOY_DISMISS });
	}, []);

	if (releaseIsLoading) {
		return <Loading />;
	}

	if (!release) throw new Error('<EnvEditor> Error: Unexpected lack of release');

	return (
		<>
			{isDeploying ? (
				<RightOverlay onClose={handleDeployDismiss}>
					<CreateDeployment appName={appName} newRelease={newRelease} dispatch={dispatch} />
				</RightOverlay>
			) : null}
			<KeyValueEditor
				state={kvState}
				dispatch={dispatch}
				keyPlaceholder="ENV key"
				valuePlaceholder="ENV value"
				conflictsMessage="Some edited keys have been updated in the latest release"
			/>
		</>
	);
}
