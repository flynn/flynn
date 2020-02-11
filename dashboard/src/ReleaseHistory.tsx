import * as React from 'react';
import * as timestamp_pb from 'google-protobuf/google/protobuf/timestamp_pb';
import styled from 'styled-components';

import { Checkmark as CheckmarkIcon } from 'grommet-icons';
import { CheckBox, Button, Grid, Box, BoxProps, Text } from 'grommet';

import ifDev from './ifDev';
import ProcessScale from './ProcessScale';
import RightOverlay from './RightOverlay';
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
	ReleaseType,
	ReleaseTypeMap,
	ScaleRequest,
	CreateScaleRequest,
	ScaleRequestState
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

enum SelectedResourceType {
	Release = 1,
	ScaleRequest
}

enum ActionType {
	SET_DEPLOY_STATUS = 'SET_DEPLOY_STATUS',
	SET_SELECTED_ITEM = 'SET_SELECTED_ITEM',
	SET_NEXT_SCALE = 'SET_NEXT_SCALE',
	SET_NEXT_RELEASE_NAME = 'SET_NEXT_RELEASE_NAME',
	SET_WINDOW = 'SET_WINDOW',
	SET_PANE_HEIGHT = 'SET_PANE_HEIGHT'
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

interface SetNextScaleAction {
	type: ActionType.SET_NEXT_SCALE;
	scale: CreateScaleRequest | null;
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
	| SetNextScaleAction
	| SetNextReleaseNameAction
	| SetWindowAction
	| SetPaneHeightAction
	| AppAction
	| AppScaleAction
	| ReleaseHistoryAction
	| CreateScaleRequestAction
	| CreateDeploymentAction;

type Dispatcher = (actions: Action | Action[]) => void;

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

			case ActionType.SET_NEXT_SCALE:
				nextState.nextScale = action.scale;
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
			//
			// <CreateScaleRequestComponent> START
			case CreateScaleRequestActionType.SET_ERROR:
				nextState.createScaleRequestError = action.error;
				return nextState;

			case CreateScaleRequestActionType.CANCEL:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' },
					{ type: ActionType.SET_NEXT_SCALE, scale: null }
				]);

			case CreateScaleRequestActionType.CREATED:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' },
					{ type: ActionType.SET_NEXT_SCALE, scale: null }
				]);
			// <CreateScaleRequestComponent> END

			// <CreateDeployment> START
			case CreateDeploymentActionType.SET_ERROR:
				nextState.createDeploymentError = action.error;
				return nextState;

			case CreateDeploymentActionType.CANCEL:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' },
					{ type: ActionType.SET_NEXT_SCALE, scale: null }
				]);

			case CreateDeploymentActionType.CREATED:
				return reducer(prevState, [
					{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
					{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' },
					{ type: ActionType.SET_NEXT_SCALE, scale: null }
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
	renderRelease: (key: string, releases: [Release, Release | null], index: number) => T;
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
			el = renderRelease(_last(d.getName().split('/')), [r as Release, pr], i);
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

/* function isWindow(obj: any): obj is Window { */
/* 	if (obj === window) return true; */
/* 	return false; */
/* } */

// TODO(jvatic): BUG: if this is rendered yesterday than it will incorrectly show "Today"
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
	release: Release;
	prevRelease: Release | null;
	onChange: (isSelected: boolean) => void;
}

const ReleaseHistoryRelease = React.memo(
	React.forwardRef(function ReleaseHistoryRelease(
		{ release: r, prevRelease: p, selected, isCurrent, onChange, ...boxProps }: ReleaseHistoryReleaseProps,
		ref: any
	) {
		return (
			<SelectableBox ref={ref} selected={selected} highlighted={isCurrent} {...boxProps}>
				<label>
					<CheckBox
						checked={selected}
						onChange={(e: React.ChangeEvent<HTMLInputElement>) => onChange(e.target.checked)}
					/>
					<ReleaseComponent release={r} prevRelease={p} />
				</label>
			</SelectableBox>
		);
	}),
	function areEqual(prevProps: ReleaseHistoryReleaseProps, nextProps: ReleaseHistoryReleaseProps) {
		if (prevProps.selected !== nextProps.selected) return false;
		if (prevProps.isCurrent !== nextProps.isCurrent) return false;
		if (prevProps.release.getName() !== nextProps.release.getName()) return false;
		if ((prevProps.prevRelease || new Release()).getName() !== (nextProps.prevRelease || new Release()).getName()) {
			return false;
		}
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
		const releaseID = s.getParent().split('/')[3];
		const diff = protoMapDiff(s.getOldProcessesMap(), s.getNewProcessesMap(), DiffOption.INCLUDE_UNCHANGED);

		const handleChange = React.useCallback(
			(e: React.ChangeEvent<HTMLInputElement>) => {
				const isSelected = e.target.checked;
				if (isSelected) {
					dispatch({
						type: ActionType.SET_SELECTED_ITEM,
						name: s.getName(),
						resourceType: SelectedResourceType.ScaleRequest
					});
				} else {
					dispatch({
						type: ActionType.SET_SELECTED_ITEM,
						name: currentReleaseName,
						resourceType: SelectedResourceType.Release
					});
				}
			},
			[s, currentReleaseName, dispatch]
		);

		return (
			<SelectableBox ref={ref} selected={selected} highlighted={isCurrent} {...boxProps}>
				<label>
					<CheckBox checked={selected} onChange={handleChange} />
					<div>
						<div>Release {releaseID}</div>
						<div>
							{(() => {
								switch (s.getState()) {
									case ScaleRequestState.SCALE_PENDING:
										return 'PENDING';
									case ScaleRequestState.SCALE_CANCELLED:
										return 'CANCELED';
									case ScaleRequestState.SCALE_COMPLETE:
										return 'COMPLETE';
									default:
										return 'UNKNOWN';
								}
							})()}
						</div>
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
										direction="row"
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
					</div>
				</label>
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
			selectedResourceType,
			nextScale,
			nextReleaseName,
			startIndex,
			length,
			paneHeight,
			selectedScaleRequestDiff,

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

	const { urlParams } = useRouter();
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

	const submitHandler = (e: React.SyntheticEvent) => {
		e.preventDefault();

		if (selectedItemName === '') {
			return;
		}

		const actions: Action[] = [];
		actions.push({ type: ActionType.SET_NEXT_RELEASE_NAME, name: selectedItemName });
		actions.push({ type: ActionType.SET_NEXT_SCALE, scale: null });
		actions.push({ type: ActionType.SET_DEPLOY_STATUS, isDeploying: true });
		dispatch(actions);
	};

	const handleScaleBtnClick = (e: React.SyntheticEvent) => {
		e.preventDefault();

		const actions: Action[] = [];
		const item = items.find((sr) => sr.getName() === selectedItemName);
		const sr = item && item.isScaleRequest ? item.getScaleRequest() : null;
		const nextScale = new CreateScaleRequest();
		if (!sr) {
			return;
		}
		nextScale.setParent(sr.getParent());
		protoMapReplace(nextScale.getProcessesMap(), sr.getNewProcessesMap());
		protoMapReplace(nextScale.getTagsMap(), sr.getNewTagsMap());
		actions.push({ type: ActionType.SET_NEXT_SCALE, scale: nextScale });
		actions.push({ type: ActionType.SET_NEXT_RELEASE_NAME, name: currentReleaseName });
		actions.push({ type: ActionType.SET_DEPLOY_STATUS, isDeploying: true });
		dispatch(actions);
	};

	const handleDeployCancel = () => {
		dispatch([
			{ type: ActionType.SET_DEPLOY_STATUS, isDeploying: false },
			{ type: ActionType.SET_NEXT_RELEASE_NAME, name: '' },
			{ type: ActionType.SET_NEXT_SCALE, scale: null }
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
			{isDeploying ? (
				<RightOverlay onClose={handleDeployCancel}>
					{selectedResourceType === SelectedResourceType.ScaleRequest &&
					nextReleaseName &&
					nextReleaseName === currentReleaseName &&
					nextScale ? (
						<CreateScaleRequestComponent appName={appName} nextScale={nextScale} dispatch={dispatch} />
					) : (
						<CreateDeployment appName={appName} releaseName={nextReleaseName} dispatch={dispatch} />
					)}
				</RightOverlay>
			) : null}

			<form onSubmit={submitHandler}>
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
								renderRelease: (key, [r, p], index) => (
									<WindowedListItem key={key} index={index} {...windowedListItemProps}>
										{(ref) => (
											<ReleaseHistoryRelease
												ref={ref}
												tag="li"
												flex={false}
												margin={{ bottom: 'small' }}
												release={r}
												prevRelease={p}
												selected={selectedItemName === r.getName()}
												isCurrent={currentReleaseName === r.getName()}
												onChange={(isSelected) => {
													if (isSelected) {
														dispatch({
															type: ActionType.SET_SELECTED_ITEM,
															name: r.getName(),
															resourceType: SelectedResourceType.Release
														});
													} else {
														dispatch({
															type: ActionType.SET_SELECTED_ITEM,
															name: currentReleaseName,
															resourceType: SelectedResourceType.Release
														});
													}
												}}
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

				<StickyBox bottom="0px" background="background" pad="xsmall" width="medium">
					{selectedResourceType === SelectedResourceType.ScaleRequest ? (
						<Box direction="row">
							<Button
								type="submit"
								disabled={selectedItemName.startsWith(currentReleaseName)}
								primary
								icon={<CheckmarkIcon />}
								label="Deploy Release"
							/>
							&nbsp;
							<Button
								type="button"
								disabled={(selectedScaleRequestDiff as Diff<string, number>).length === 0}
								onClick={handleScaleBtnClick}
								icon={<CheckmarkIcon />}
								label="Scale"
							/>
						</Box>
					) : (
						<Button
							type="submit"
							disabled={selectedItemName === currentReleaseName}
							primary
							icon={<CheckmarkIcon />}
							label="Deploy Release"
						/>
					)}
				</StickyBox>
			</form>
		</>
	);
}
export default React.memo(ReleaseHistory, function areEqual(prevProps: Props, nextProps: Props) {
	return prevProps.appName !== nextProps.appName;
});

ifDev(() => ((ReleaseHistory as any).whyDidYouRender = true));
