import * as React from 'react';
import { Route } from 'react-router-dom';
import * as timestamp_pb from 'google-protobuf/google/protobuf/timestamp_pb';
import styled from 'styled-components';

import { Grid, Box, BoxProps, Text } from 'grommet';

import ifDev from './ifDev';
import ProcessScale from './ProcessScale';
import RightOverlay from './RightOverlay';
import ExpandedRelease, {
	Action as ExpandedReleaseAction,
	ActionType as ExpandedReleaseActionType
} from './ExpandedRelease';
import ExpandedScaleRequestComponent, {
	Action as ExpandedScaleRequestAction,
	ActionType as ExpandedScaleRequestActionType
} from './ExpandedScaleRequest';
import { default as useRouter } from './useRouter';
import { useAppWithDispatch, Action as AppAction, ActionType as AppActionType } from './useApp';
import {
	useReleaseHistoryWithDispatch,
	Action as ReleaseHistoryAction,
	ActionType as ReleaseHistoryActionType,
	reducer as releaseHistoryReducer,
	State as ReleaseHistoryState,
	initialState as initialReleaseHistoryState
} from './useReleaseHistory';
import { useAppScaleWithDispatch, Action as AppScaleAction, ActionType as AppScaleActionType } from './useAppScale';
import useErrorHandler from './useErrorHandler';
import useWithCancel from './useWithCancel';
import useDateString from './useDateString';
import { listDeploymentsRequestFilterType, ReleaseHistoryItem } from './client';
import {
	App,
	Release,
	ExpandedDeployment,
	ReleaseType,
	ReleaseTypeMap,
	ScaleRequest,
	CreateScaleRequest
} from './generated/controller_pb';
import Loading from './Loading';
import CreateDeployment, {
	Action as CreateDeploymentAction,
	ActionType as CreateDeploymentActionType
} from './CreateDeployment';
import CreateScaleRequestComponent, {
	ActionType as CreateScaleRequestActionType,
	Action as CreateScaleRequestAction
} from './CreateScaleRequest';
import ReleaseComponent from './Release';
import WindowedListState from './WindowedListState';
import WindowedList, { WindowedListItem } from './WindowedList';
import protoMapDiff, { Diff, DiffOp, DiffOption } from './util/protoMapDiff';
import protoMapReplace from './util/protoMapReplace';
import isActionType from './util/isActionType';
import roundedDate from './util/roundedDate';
import timestampToDate from './util/timestampToDate';

enum SelectedResourceType {
	Release = 1,
	ScaleRequest
}

enum ActionType {
	SET_DEPLOY_STATUS = 'ReleaseHistory__SET_DEPLOY_STATUS',
	SET_SELECTED_ITEM = 'ReleaseHistory__SET_SELECTED_ITEM',
	SET_NEXT_RELEASE_NAME = 'ReleaseHistory__SET_NEXT_RELEASE_NAME',
	SET_WINDOW = 'ReleaseHistory__SET_WINDOW',
	SET_PANE_HEIGHT = 'ReleaseHistory__SET_PANE_HEIGHT'
}

interface SetDeployStatusAction {
	type: ActionType.SET_DEPLOY_STATUS;
	isDeploying: boolean;
}

interface SetSelectedItemAction {
	type: ActionType.SET_SELECTED_ITEM;
	name: string;
	resourceType?: SelectedResourceType;
}

interface SetNextReleaseNameAction {
	type: ActionType.SET_NEXT_RELEASE_NAME;
	name: string;
}

interface SetWindowAction {
	type: ActionType.SET_WINDOW;
	startIndex: number;
	length: number;
}

interface SetPaneHeightAction {
	type: ActionType.SET_PANE_HEIGHT;
	height: number;
}

type Action =
	| SetDeployStatusAction
	| SetSelectedItemAction
	| SetNextReleaseNameAction
	| SetWindowAction
	| SetPaneHeightAction
	| AppAction
	| AppScaleAction
	| ReleaseHistoryAction
	| ExpandedReleaseAction
	| ExpandedScaleRequestAction
	| CreateScaleRequestAction
	| CreateDeploymentAction;

type Dispatcher = (actions: Action | Action[]) => void;

enum SelectionType {
	RELEASE = 'RELEASE',
	SCALE = 'SCALE'
}

interface ReleaseSelection {
	type: SelectionType.RELEASE;
	release: Release;
	prevRelease?: Release | null;
}

interface ScaleSelection {
	type: SelectionType.SCALE;
	scale: ScaleRequest;
}

interface State {
	isDeploying: boolean;
	selectedItemName: string;
	selectedResourceType: SelectedResourceType;
	nextScale: CreateScaleRequest | null;
	nextReleaseName: string;
	startIndex: number;
	length: number;
	paneHeight: number;
	selectedScaleRequestDiff: Diff<string, number>;

	// useApp
	app: App | null;
	appLoading: boolean;
	appError: Error | null;

	// useAppScale
	currentScale: ScaleRequest | null;
	currentScaleLoading: boolean;
	currentScaleError: Error | null;

	// useReleaseHistory
	releaseHistoryState: ReleaseHistoryState;

	// <CreateScaleRequest>
	createScaleRequestError: Error | null;

	// <CreateDeployment>
	createDeploymentError: Error | null;
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

const emptyScaleRequestDiff: Diff<string, number> = [];
function initialState(): State {
	return {
		isDeploying: false,
		selectedItemName: '',
		selectedResourceType: SelectedResourceType.Release,
		nextScale: null,
		nextReleaseName: '',
		startIndex: 0,
		length: 0,
		paneHeight: 400,
		selectedScaleRequestDiff: emptyScaleRequestDiff,

		// useApp
		app: null,
		appLoading: true,
		appError: null,

		// useAppScale
		currentScale: null,
		currentScaleLoading: true,
		currentScaleError: null,

		// useReleaseHistory
		releaseHistoryState: initialReleaseHistoryState(),

		// <CreateScaleRequest>
		createScaleRequestError: null,

		// <CreateDeployment>
		createDeploymentError: null
	};
}

function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_DEPLOY_STATUS:
				nextState.isDeploying = action.isDeploying;
				return nextState;

			case ActionType.SET_SELECTED_ITEM:
				nextState.selectedItemName = action.name;
				if (action.resourceType) {
					nextState.selectedResourceType = action.resourceType;
				}
				return nextState;

			case ActionType.SET_NEXT_RELEASE_NAME:
				nextState.nextReleaseName = action.name;
				return nextState;

			case ActionType.SET_WINDOW:
				if (prevState.startIndex === action.startIndex && prevState.length === action.length) {
					return prevState;
				}
				nextState.startIndex = action.startIndex;
				nextState.length = action.length;
				return nextState;

			case ActionType.SET_PANE_HEIGHT:
				nextState.paneHeight = action.height;
				return nextState;

			// useApp START
			case AppActionType.SET_APP:
				if (action.app) {
					nextState.app = action.app;
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

			// useAppScale START
			case AppScaleActionType.SET_SCALE:
				nextState.currentScale = action.scale;
				return nextState;

			case AppScaleActionType.SET_ERROR:
				nextState.currentScaleError = action.error;
				return nextState;

			case AppScaleActionType.SET_LOADING:
				nextState.currentScaleLoading = action.loading;
				return nextState;
			// useAppScale END

			// <ExpandedRelease> START
			case ExpandedReleaseActionType.DEPLOY_RELEASE:
				nextState.isDeploying = true;
				nextState.nextReleaseName = action.releaseName;
				return nextState;
			// <ExpandedRelease> END

			// <ExpandedScaleRequest> START
			case ExpandedScaleRequestActionType.DEPLOY_SCALE:
				nextState.isDeploying = true;
				nextState.nextScale = ((s) => {
					const nextScale = new CreateScaleRequest();
					protoMapReplace(nextScale.getProcessesMap(), s.getNewProcessesMap());
					protoMapReplace(nextScale.getTagsMap(), s.getNewTagsMap());
					return nextScale;
				})(action.scale);
				return nextState;
			// <ExpandedScaleRequest> END

			// <CreateScaleRequestComponent> START
			case CreateScaleRequestActionType.SET_ERROR:
				nextState.createScaleRequestError = action.error;
				return nextState;

			case CreateScaleRequestActionType.CANCEL:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' }
				]);

			case CreateScaleRequestActionType.CREATED:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' }
				]);
			// <CreateScaleRequestComponent> END

			// <CreateDeployment> START
			case CreateDeploymentActionType.SET_ERROR:
				nextState.createDeploymentError = action.error;
				return nextState;

			case CreateDeploymentActionType.CANCEL:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' }
				]);

			case CreateDeploymentActionType.CREATED:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' }
				]);
			// <CreateDeployment> END

			default:
				// useReleaseHistory
				if (isActionType<ReleaseHistoryAction>(ReleaseHistoryActionType, action)) {
					nextState.releaseHistoryState = releaseHistoryReducer(prevState.releaseHistoryState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		if (nextState.selectedResourceType === SelectedResourceType.ScaleRequest) {
			const item = nextState.releaseHistoryState.allItems.find((sr) => sr.getName() === nextState.selectedItemName);
			const sr = item && item.isScaleRequest ? item.getScaleRequest() : null;
			if (sr) {
				const diff = protoMapDiff(
					(nextState.currentScale as ScaleRequest).getNewProcessesMap(),
					sr.getNewProcessesMap()
				);
				nextState.selectedScaleRequestDiff = diff.length ? diff : emptyScaleRequestDiff;
				return;
			}
		}
		nextState.selectedScaleRequestDiff = emptyScaleRequestDiff;
	})();

	return nextState;
}

interface MapHistoryProps<T> {
	startIndex: number;
	length: number;
	items: ReleaseHistoryItem[];
	renderDate: (key: string, date: Date) => T;
	renderRelease: (key: string, releases: [Release, Release | null, ExpandedDeployment], index: number) => T;
	renderScale: (key: string, scaleRequest: ScaleRequest, index: number) => T;
}

function _last<T>(arr: Array<T>): T {
	return arr[arr.length - 1];
}

function mapHistory<T>({
	startIndex,
	length,
	items,
	renderRelease,
	renderScale,
	renderDate
}: MapHistoryProps<T>): Array<T | null> {
	const res = [] as Array<T | null>;
	const len = Math.min(startIndex + length, items.length);
	let date: Date | null = null;
	for (let i = startIndex; i < len; i++) {
		const item = items[i];
		let prevDate = date;
		let el: T | null = null;
		if (item.isScaleRequest) {
			const s = item.getScaleRequest();
			date = roundedDate((s.getCreateTime() as timestamp_pb.Timestamp).toDate());
			el = renderScale(_last(s.getName().split('/')) + `-${s.getState()}`, s, i);
		} else {
			// it must be a deployment
			const d = item.getDeployment();
			const r = d.getNewRelease() || null;
			const pr = d.getOldRelease() || null;
			date = roundedDate((d.getCreateTime() as timestamp_pb.Timestamp).toDate());
			el = renderRelease(_last(d.getName().split('/')), [r as Release, pr, d], i);
		}

		if (prevDate === null || date < prevDate) {
			res.push(renderDate(date.toDateString(), date));
		}

		res.push(el);
	}

	return res;
}

interface SelectableBoxProps {
	selected: boolean;
	highlighted: boolean;
}

const selectedBoxCSS = `
	background-color: var(--active);
`;

const highlightedBoxCSS = `
	border-left: 4px solid var(--brand);
`;

const nonHighlightedBoxCSS = `
	border-left: 4px solid transparent;
`;

const SelectableBox = styled(Box)`
	&:hover {
		background-color: var(--active);
	}
	padding-left: 2px;

	${(props: SelectableBoxProps) => (props.selected ? selectedBoxCSS : '')};
	${(props: SelectableBoxProps) => (props.highlighted ? highlightedBoxCSS : nonHighlightedBoxCSS)};
`;

interface StickyBoxProps {
	top?: string;
	bottom?: string;
}

const StickyBox = styled(Box)`
	position: sticky;
	${(props: StickyBoxProps) => (props.top ? 'top: ' + props.top + ';' : '')} ${(props: StickyBoxProps) =>
		props.bottom ? 'bottom: ' + props.bottom + ';' : ''};
`;

const StyledDateHeaderBox = styled(StickyBox)`
	margin: 0;
	&:before {
		position: absolute;
		display: block;
		content: ' ';
		width: 100%;
		height: 50%;
		top: 0px;
		border-bottom: 1px solid var(--dark-6);
		background-color: var(--background);
		z-index: 1000;
	}
`;

interface ReleaseHistoryDateHeaderProps extends BoxProps {
	date: Date;
}

function ReleaseHistoryDateHeader({ date, ...boxProps }: ReleaseHistoryDateHeaderProps) {
	const dateString = useDateString(date);
	// NOTE: We need to unset min-height for the <Box /> below as it is otherwise
	// set to 0 which causes the content to overflow the box.
	return (
		<StyledDateHeaderBox top="-1px" style={{ minHeight: 'unset' }} {...boxProps}>
			<Box alignSelf="center" round background="background" pad="small" style={{ zIndex: 1002 }}>
				{dateString}
			</Box>
		</StyledDateHeaderBox>
	);
}

interface ReleaseHistoryReleaseProps extends BoxProps {
	selected: boolean;
	isCurrent: boolean;
	deployment: ExpandedDeployment;
	dispatch: Dispatcher;
}

const ReleaseHistoryRelease = React.memo(
	React.forwardRef(function ReleaseHistoryRelease(
		{ deployment, selected, isCurrent, dispatch, ...boxProps }: ReleaseHistoryReleaseProps,
		ref: any
	) {
		const release = deployment.getNewRelease();
		const prevRelease = deployment.getOldRelease() || null;
		const { urlParams, history } = useRouter();
		const handleClick = React.useCallback(() => {
			history.push({ pathname: `/${deployment.getName()}`, search: urlParams.toString() });
		}, [deployment, urlParams, history]);
		if (release === undefined) return null;
		return (
			<SelectableBox ref={ref} selected={selected} highlighted={isCurrent} {...boxProps} onClick={handleClick}>
				<ReleaseComponent
					timestamp={timestampToDate(deployment.getCreateTime())}
					release={release}
					prevRelease={prevRelease}
				/>
			</SelectableBox>
		);
	}),
	function areEqual(prevProps: ReleaseHistoryReleaseProps, nextProps: ReleaseHistoryReleaseProps) {
		if (prevProps.selected !== nextProps.selected) return false;
		if (prevProps.isCurrent !== nextProps.isCurrent) return false;
		if (prevProps.deployment.getName() !== nextProps.deployment.getName()) return false;
		return true;
	}
);
ifDev(() => ((ReleaseHistoryRelease as any).whyDidYouRender = true));

interface ReleaseHistoryScaleProps extends BoxProps {
	selected: boolean;
	isCurrent: boolean;
	scaleRequest: ScaleRequest;
	currentReleaseName: string;
	dispatch: Dispatcher;
}

const ReleaseHistoryScale = React.memo(
	React.forwardRef(function ReleaseHistoryScale(
		{ scaleRequest: s, selected, isCurrent, currentReleaseName, dispatch, ...boxProps }: ReleaseHistoryScaleProps,
		ref: any
	) {
		const { urlParams, history } = useRouter();
		const handleClick = React.useCallback(() => {
			history.push({ pathname: `/${s.getName()}`, search: urlParams.toString() });
		}, [s, urlParams, history]);

		const diff = protoMapDiff(s.getOldProcessesMap(), s.getNewProcessesMap(), DiffOption.INCLUDE_UNCHANGED);

		return (
			<SelectableBox ref={ref} selected={selected} highlighted={isCurrent} {...boxProps} onClick={handleClick}>
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
			</SelectableBox>
		);
	}),
	function areEqual(prevProps: ReleaseHistoryScaleProps, nextProps: ReleaseHistoryScaleProps) {
		if (prevProps.selected !== nextProps.selected) return false;
		if (prevProps.isCurrent !== nextProps.isCurrent) return false;
		if (prevProps.scaleRequest.getName() !== nextProps.scaleRequest.getName()) return false;
		return true;
	}
);
ifDev(() => ((ReleaseHistoryScale as any).whyDidYouRender = true));

export interface Props {
	appName: string;
}

function ReleaseHistory({ appName }: Props) {
	const [
		{
			isDeploying,
			selectedItemName,
			nextScale,
			nextReleaseName,
			startIndex,
			length,
			paneHeight,

			// useApp
			app,
			appLoading,
			appError,

			// useAppScale
			currentScale,
			currentScaleLoading,
			currentScaleError,

			// useReleaseHistory
			releaseHistoryState: {
				allItems: items,
				nextPageToken,
				fetchNextPage,
				loading: releaseHistoryLoading,
				error: releaseHistoryError
			},

			// <CreateDeployment>
			createDeploymentError
		},
		dispatch
	] = React.useReducer<Reducer>(reducer, initialState());

	const handleError = useErrorHandler();
	React.useEffect(() => {
		const error = appError || currentScaleError || releaseHistoryError || createDeploymentError;
		let cancel = () => {};
		if (error) {
			cancel = handleError(error);
		}
		return cancel;
	}, [appError, currentScaleError, releaseHistoryError, createDeploymentError, handleError]);

	useAppWithDispatch(appName, dispatch);

	// Get current formation
	useAppScaleWithDispatch(appName, dispatch);

	const currentReleaseName = app ? app.getRelease() : '';

	React.useEffect(() => {
		if (!currentReleaseName) return;
		dispatch({ type: ActionType.SET_SELECTED_ITEM, name: currentReleaseName });
	}, [currentReleaseName]);

	const {
		urlParams,
		match: { path: parentRoutePath },
		history
	} = useRouter();
	const releasesListFilters = [urlParams.getAll('rhf'), ['code', 'env', 'scale']].find((i) => i.length > 0) as string[];

	const rhf = releasesListFilters;
	const isCodeReleaseEnabled = React.useMemo(() => {
		return rhf.length === 0 || rhf.indexOf('code') !== -1;
	}, [rhf]);
	const isConfigReleaseEnabled = React.useMemo(() => {
		return rhf.indexOf('env') !== -1;
	}, [rhf]);
	const scalesEnabled = React.useMemo(() => {
		return rhf.indexOf('scale') !== -1;
	}, [rhf]);

	// Stream release history (scales and deployments coalesced together)
	const deploymentsEnabled = isCodeReleaseEnabled || isConfigReleaseEnabled;
	const deploymentReqModifiers = React.useMemo(() => {
		let filterType = ReleaseType.ANY as ReleaseTypeMap[keyof ReleaseTypeMap];
		if (isCodeReleaseEnabled && !isConfigReleaseEnabled) {
			filterType = ReleaseType.CODE;
		} else if (isConfigReleaseEnabled && !isCodeReleaseEnabled) {
			filterType = ReleaseType.CONFIG;
		}

		return [listDeploymentsRequestFilterType(filterType)];
	}, [isCodeReleaseEnabled, isConfigReleaseEnabled]);
	const scaleReqModifiers = React.useMemo(() => [], []);
	useReleaseHistoryWithDispatch(
		appName,
		scaleReqModifiers,
		deploymentReqModifiers,
		scalesEnabled,
		deploymentsEnabled,
		dispatch
	);

	const handleSelectionCancel = () => {
		history.push({ pathname: `/${appName}`, search: urlParams.toString() });
	};

	const handleDeployCancel = () => {
		dispatch([
			{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
			{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' }
		]);
	};

	const paddingTopRef = React.useRef<HTMLElement>();
	const paddingBottomRef = React.useRef<HTMLElement>();

	const windowedListState = React.useMemo(() => new WindowedListState(), []);
	React.useEffect(() => {
		return windowedListState.onChange((state: WindowedListState) => {
			const paddingTopNode = paddingTopRef.current;
			if (paddingTopNode) {
				paddingTopNode.style.height = state.paddingTop + 'px';
			}
			const paddingBottomNode = paddingBottomRef.current;
			if (paddingBottomNode) {
				paddingBottomNode.style.height = state.paddingBottom + 'px';
			}

			dispatch({ type: ActionType.SET_WINDOW, startIndex: state.visibleIndexTop, length: state.visibleLength });
		});
	}, [windowedListState]);

	// pagination
	const withCancel = useWithCancel();
	React.useEffect(() => {
		if (nextPageToken && startIndex + length >= items.length - 10) {
			withCancel.set(nextPageToken.toString(), fetchNextPage(nextPageToken));
		}
		return () => {};
	}, [fetchNextPage, items.length, length, nextPageToken, withCancel, startIndex]);

	const releaseHistoryScrollContainerRef = React.useRef<HTMLElement>();
	const [releaseHistoryScrollContainerNode, setReleaseHistoryScrollContainerNode] = React.useState<HTMLElement | null>(
		null
	);
	React.useEffect(() => {
		// this is called after every render
		// triggers the useEffect below if/when the ref changes
		setReleaseHistoryScrollContainerNode(releaseHistoryScrollContainerRef.current || null);
	}, undefined); // eslint-disable-line react-hooks/exhaustive-deps

	const minPaneHeight = 400;
	const windowingThresholdTop = 600;
	const windowingThresholdBottom = 600;
	const resizeObserverRef = React.useMemo(() => ({ current: null as ResizeObserver | null }), []);
	React.useEffect(() => {
		function adjustHeights() {
			let offsetTop = 128;
			if (releaseHistoryScrollContainerNode) {
				const rect = releaseHistoryScrollContainerNode.getBoundingClientRect();
				windowedListState.viewportHeight = rect.height + windowingThresholdTop + windowingThresholdBottom;

				// set `offsetTop` to scrollContainer.top - paneContainer.top
				let node = releaseHistoryScrollContainerNode as HTMLElement | null;
				for (let i = 0; i < 4; i++) {
					if (!node) break;
					node = node.parentElement;
				}
				if (node) {
					const r = node.getBoundingClientRect();
					offsetTop = rect.top - r.top;
				}
			}

			// adjust Release History pane height to fill available space
			const adjustedHeight = Math.max(minPaneHeight, document.documentElement.clientHeight - offsetTop);
			if (paneHeight !== adjustedHeight) {
				dispatch({ type: ActionType.SET_PANE_HEIGHT, height: adjustedHeight });
			}
		}
		adjustHeights();

		if (!resizeObserverRef.current) {
			const resizeObserver = new window.ResizeObserver(() => {
				adjustHeights();
			});
			resizeObserver.observe(document.body);
			resizeObserverRef.current = resizeObserver;

			withCancel.set('resizeObserver', () => {
				resizeObserver.disconnect();
			});
		}

		windowedListState.length = items.length;
		windowedListState.defaultHeight = 150;
		windowedListState.calculateVisibleIndices();
	}, [items.length, paneHeight, releaseHistoryScrollContainerNode, windowedListState, resizeObserverRef, withCancel]);

	if (releaseHistoryLoading || currentScaleLoading || appLoading) {
		return <Loading />;
	}

	return (
		<>
			<Route path={`${parentRoutePath}/deployments/:deploymentID`}>
				<RightOverlay onClose={handleSelectionCancel}>
					<ExpandedRelease dispatch={dispatch} />
				</RightOverlay>
			</Route>

			<Route path={`${parentRoutePath}/releases/:releaseID/scales/:scaleRequestID`}>
				<RightOverlay onClose={handleSelectionCancel}>
					<ExpandedScaleRequestComponent dispatch={dispatch} />
				</RightOverlay>
			</Route>

			{isDeploying ? (
				<RightOverlay onClose={handleDeployCancel}>
					{nextScale ? (
						<CreateScaleRequestComponent appName={appName} nextScale={nextScale} dispatch={dispatch} />
					) : (
						<CreateDeployment appName={appName} releaseName={nextReleaseName} dispatch={dispatch} />
					)}
				</RightOverlay>
			) : null}

			<Box
				ref={releaseHistoryScrollContainerRef as any}
				tag="ul"
				flex={false}
				alignContent="start"
				overflow={{ vertical: 'scroll', horizontal: 'auto' }}
				style={{
					position: 'relative',
					height: paneHeight,
					padding: 0,
					margin: 0
				}}
			>
				<Box tag="li" ref={paddingTopRef as any} style={{ height: windowedListState.paddingTop }} flex={false}>
					&nbsp;
				</Box>
				<WindowedList state={windowedListState} thresholdTop={windowingThresholdTop}>
					{(windowedListItemProps) => {
						return mapHistory({
							startIndex,
							length,
							items,
							renderDate: (key, date) => <ReleaseHistoryDateHeader key={key} date={date} tag="li" margin="xsmall" />,
							renderRelease: (key, [r, p, d], index) => (
								<WindowedListItem key={key} index={index} {...windowedListItemProps}>
									{(ref) => (
										<ReleaseHistoryRelease
											ref={ref}
											tag="li"
											flex={false}
											margin={{ bottom: 'small' }}
											deployment={d}
											selected={selectedItemName === r.getName()}
											isCurrent={currentReleaseName === r.getName()}
											dispatch={dispatch}
										/>
									)}
								</WindowedListItem>
							),
							renderScale: (key, s, index) => (
								<WindowedListItem key={key} index={index} {...windowedListItemProps}>
									{(ref) => (
										<ReleaseHistoryScale
											ref={ref}
											tag="li"
											flex={false}
											margin={{ bottom: 'small' }}
											scaleRequest={s}
											currentReleaseName={currentReleaseName}
											selected={selectedItemName === s.getName()}
											isCurrent={currentScale ? currentScale.getName() === s.getName() : false}
											dispatch={dispatch}
										/>
									)}
								</WindowedListItem>
							)
						});
					}}
				</WindowedList>
				<Box tag="li" ref={paddingBottomRef as any} style={{ height: windowedListState.paddingBottom }} flex={false}>
					&nbsp;
				</Box>
			</Box>
		</>
	);
}
export default React.memo(ReleaseHistory, function areEqual(prevProps: Props, nextProps: Props) {
	return prevProps.appName !== nextProps.appName;
});

ifDev(() => ((ReleaseHistory as any).whyDidYouRender = true));
