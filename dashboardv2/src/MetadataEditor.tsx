import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Checkmark as CheckmarkIcon } from 'grommet-icons';
import { Box, Button } from 'grommet';
import {
	useAppWithDispatch,
	State as AppState,
	reducer as appReducer,
	initialState as initialAppState,
	Action as AppAction,
	ActionType as AppActionType
} from './useApp';
import useClient from './useClient';
import useWithCancel from './useWithCancel';
import useNavProtection from './useNavProtection';
import useErrorHandler from './useErrorHandler';
import Loading from './Loading';
import KeyValueEditor, {
	State as KVState,
	initialState as initialKVState,
	buildState as buildKVState,
	reducer as kvReducer,
	Data,
	DataAction,
	DataActionType,
	Action as KVEditorAction,
	ActionType as KVEditorActionType,
	isEditorAction as isKVActionType,
	Suggestion,
	buildData,
	rebaseData,
	getEntries
} from './KeyValueEditor';
import KeyValueDiff from './KeyValueDiff';
import protoMapReplace from './util/protoMapReplace';
import isActionType from './util/isActionType';
import { App } from './generated/controller_pb';
import RightOverlay from './RightOverlay';

interface Props {
	appName: string;
}

interface State {
	appState: AppState;
	kvState: KVState;
	isConfirming: boolean;
	isDeploying: boolean;
}

function initialState(props: Props): State {
	const suggestions = [
		{
			key: 'github.url',
			validateValue: (value: string) => {
				if (value.match(/^https:\/\/github\.com\/[^/]+\/[^/]+$/)) {
					return null;
				}
				return 'invalid github repo URL';
			},
			valueTemplate: {
				value: 'https://github.com/ORG/REPO',
				selectionStart: 19,
				selectionEnd: 27,
				direction: 'forward'
			}
		} as Suggestion
	];
	return {
		appState: initialAppState(),
		kvState: initialKVState({ suggestions }),
		isConfirming: false,
		isDeploying: false
	};
}

enum ActionType {
	DEPLOY = 'DEPLOY',
	DEPLOY_ERROR = 'DEPLOY_ERROR',
	DEPLOY_SUCCESS = 'DEPLOY_SUCCESS',
	CONFIRM_CANCEL = 'CONFIRM_CANCEL'
}

interface DeployAction {
	type: ActionType.DEPLOY;
}

interface DeployErrorAction {
	type: ActionType.DEPLOY_ERROR;
	error: Error;
}

interface DeploySuccessAction {
	type: ActionType.DEPLOY_SUCCESS;
}

interface ConfirmCancelAction {
	type: ActionType.CONFIRM_CANCEL;
}

type Action =
	| DeployAction
	| DeployErrorAction
	| DeploySuccessAction
	| ConfirmCancelAction
	| AppAction
	| DataAction
	| KVEditorAction;

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.DEPLOY:
				nextState.isDeploying = true;
				return nextState;
			case ActionType.DEPLOY_ERROR:
				nextState.isConfirming = false;
				nextState.isDeploying = true;
				return nextState;
			case ActionType.DEPLOY_SUCCESS:
				nextState.isConfirming = false;
				nextState.isDeploying = false;
				return nextState;
			case KVEditorActionType.SUBMIT_DATA:
				nextState.kvState = kvReducer(
					buildKVState(prevState.kvState, Object.assign({}, prevState.kvState, { data: action.data })),
					action
				);
				nextState.isConfirming = nextState.kvState.data.hasChanges;
				return nextState;
			case ActionType.CONFIRM_CANCEL:
				nextState.isConfirming = false;
				return nextState;
			default:
				if (isActionType<AppAction>(AppActionType, action)) {
					nextState.appState = appReducer(prevState.appState, action);
					return nextState;
				}

				if (isKVActionType(action)) {
					nextState.kvState = kvReducer(prevState.kvState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		const {
			appState: { app },
			kvState: { data }
		} = nextState;
		const {
			appState: { app: prevApp }
		} = prevState;
		if (!app) return;
		if (app === prevApp) return;

		if (!prevApp) {
			// handle setting initial data
			nextState.kvState.data = buildData(app.getLabelsMap().toArray());
		} else {
			// handle app labels being updated elsewhere
			nextState.kvState.data = rebaseData(data, app.getLabelsMap().toArray());
		}
	})();

	return nextState;
}

function MetadataEditor(props: Props) {
	const { appName } = props;
	const [
		{
			kvState,
			isConfirming,
			isDeploying,
			appState: { app, loading: isLoading, error: appError }
		},
		dispatch
	] = React.useReducer(reducer, initialState(props));
	const data = kvState.data;
	useAppWithDispatch(appName, dispatch);
	const client = useClient();
	const withCancel = useWithCancel();
	const handleError = useErrorHandler();

	React.useEffect(() => {
		if (!appError) return () => {};
		return handleError(appError);
	}, [appError, handleError]);

	const [enableNavProtection, disableNavProtection] = useNavProtection();
	React.useEffect(() => {
		if (data && data.hasChanges) {
			enableNavProtection();
		} else {
			disableNavProtection();
		}
	}, [data, disableNavProtection, enableNavProtection]);

	const handleConfirmSubmit = React.useCallback(
		(event: React.SyntheticEvent) => {
			event.preventDefault();
			const app = new App();
			app.setName(appName);
			protoMapReplace(app.getLabelsMap(), new jspb.Map(getEntries(data as Data)));
			dispatch({ type: ActionType.DEPLOY });
			const cancelKey = `updateApp(${app.getName()})`;
			withCancel.call(`${cancelKey}.error`);
			const cancel = client.updateApp(app, (app: App, error: Error | null) => {
				if (error) {
					dispatch({ type: ActionType.DEPLOY_ERROR, error });
					const cancel = handleError(error);
					withCancel.set(`${cancelKey}.error`, cancel);
					return;
				}
				dispatch([
					{ type: ActionType.DEPLOY_SUCCESS },
					{ type: DataActionType.REBASE, base: app.getLabelsMap().toArray() }
				]);
			});
			withCancel.set(cancelKey, cancel);
		},
		[appName, withCancel, client, data, handleError]
	);

	const handleCancelBtnClick = React.useCallback((event?: React.SyntheticEvent) => {
		if (event) {
			event.preventDefault();
		}
		dispatch({ type: ActionType.CONFIRM_CANCEL });
	}, []);

	function renderDeployMetadata() {
		if (!app || !data) return;
		return (
			<Box tag="form" fill direction="column" onSubmit={handleConfirmSubmit}>
				<Box flex="grow">
					<h3>Review Changes</h3>
					<KeyValueDiff prev={app.getLabelsMap()} next={new jspb.Map(getEntries(data))} />
				</Box>
				<Box fill="horizontal" direction="row" align="end" gap="small" justify="between">
					<Button
						type="submit"
						disabled={isDeploying}
						primary
						icon={<CheckmarkIcon />}
						label={isDeploying ? 'Saving...' : 'Save'}
					/>
					<Button type="button" label="Cancel" onClick={handleCancelBtnClick} />
				</Box>
			</Box>
		);
	}

	if (isLoading) {
		return <Loading />;
	}

	return (
		<>
			{isConfirming ? <RightOverlay onClose={handleCancelBtnClick}>{renderDeployMetadata()}</RightOverlay> : null}
			<KeyValueEditor state={kvState} dispatch={dispatch} />
		</>
	);
}
export default React.memo(MetadataEditor);

(MetadataEditor as any).whyDidYouRender = true;
