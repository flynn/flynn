import * as React from 'react';
import fz from 'fz';
import { Box } from 'grommet';
import styled from 'styled-components';

import useRouter from './useRouter';
import {
	useAppsListWithDispatch,
	ActionType as AppsActionType,
	Action as AppsAction,
	reducer as appsReducer,
	initialState as initialAppsState,
	State as AppsState
} from './useAppsList';
import useErrorHandler from './useErrorHandler';
import useDebouncedInputOnChange from './useDebouncedInputOnChange';

import isActionType from './util/isActionType';

import { App } from './generated/controller_pb';
import { excludeAppsWithLabels } from './client';

import Loading from './Loading';
import NavAnchor from './NavAnchor';
import { TextInput } from './GrommetTextInput';
import WindowedListState from './WindowedListState';
import WindowedList, { WindowedListItem } from './WindowedList';

const List = styled('ul')`
	margin: 0;
	padding: 0;
	overflow-y: auto;
`;

interface StyledNavAnchorProps {
	selected: boolean;
}

const selectedNavAnchorCSS = `
	color: var(--white);
`;

const StyledNavAnchor = styled(NavAnchor)`
	width: 100%;
	${(props: StyledNavAnchorProps) => (props.selected ? selectedNavAnchorCSS : '')}
`;

export enum ActionType {
	SET_START_INDEX = 'AppsListNav__SET_START_INDEX',
	SET_LENGTH = 'AppsListNav__SET_LENGTH',
	SET_FILTER = 'AppsListNav__SET_FILTER'
}

interface SetStartIndexAction {
	type: ActionType.SET_START_INDEX;
	index: number;
}

interface SetLengthAction {
	type: ActionType.SET_LENGTH;
	length: number;
}

interface SetFilterAction {
	type: ActionType.SET_FILTER;
	value: string;
}

export type Action = SetStartIndexAction | SetLengthAction | SetFilterAction | AppsAction;

type Dispatcher = (actions: Action | Action[]) => void;

interface State {
	appsState: AppsState;
	filterText: string;
	filteredApps: App[];
	startIndex: number;
	length: number;
	windowedListState: WindowedListState;
	windowingThresholdTop: number;
	windowingThresholdBottom: number;
}

function initialState(): State {
	const appsState = initialAppsState();
	return {
		appsState,
		filterText: '',
		filteredApps: appsState.allApps,
		startIndex: 0,
		length: 0,
		windowedListState: new WindowedListState(),
		windowingThresholdTop: 600,
		windowingThresholdBottom: 600
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
			case ActionType.SET_START_INDEX:
				nextState.startIndex = action.index;
				return nextState;

			case ActionType.SET_LENGTH:
				nextState.length = action.length;
				return nextState;

			case ActionType.SET_FILTER:
				nextState.filterText = action.value;
				return nextState;

			default:
				if (isActionType<AppsAction>(AppsActionType, action)) {
					nextState.appsState = appsReducer(prevState.appsState, action);
					return nextState;
				}

				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		const {
			appsState: { allApps },
			filterText
		} = nextState;
		if (allApps === prevState.appsState.allApps && filterText === prevState.filterText) {
			return;
		}
		nextState.filteredApps = allApps.filter((a) => {
			return fz(a.getDisplayName(), filterText);
		});
	})();

	return nextState;
}

export interface Props {}

export default function AppsListNav(props: Props) {
	const { location, urlParams } = useRouter();
	const [
		{
			appsState: { nextPageToken, fetchNextPage, loading: isLoading, error: appsError },
			filteredApps: apps,
			startIndex,
			length,
			windowedListState,
			windowingThresholdTop,
			windowingThresholdBottom
		},
		dispatch
	] = React.useReducer(reducer, initialState());
	const excludeSystemAppsFilter = React.useMemo(() => excludeAppsWithLabels([['flynn-system-app', 'true']]), []);
	const showSystemApps = urlParams.get('show-system-apps') === 'true';
	const appsListFilters = React.useMemo(() => (showSystemApps ? [] : [excludeSystemAppsFilter]), [
		excludeSystemAppsFilter,
		showSystemApps
	]);
	useAppsListWithDispatch(dispatch, appsListFilters);
	const handleError = useErrorHandler();
	React.useEffect(() => {
		let cancel = () => {};
		if (appsError) {
			cancel = handleError(appsError);
		}
		return cancel;
	}, [appsError, handleError]);

	const setFilterText = React.useCallback(
		(value: string) => {
			dispatch({ type: ActionType.SET_FILTER, value });
		},
		[dispatch]
	);
	const [filterText, handleFilterTextChange, flushFilterText, cancelFilterTextChange] = useDebouncedInputOnChange(
		'',
		setFilterText
	);
	React.useEffect(() => {
		return () => {
			cancelFilterTextChange();
		};
	}, []); // eslint-disable-line react-hooks/exhaustive-deps

	// some query params are persistent, make sure they're passed along if present
	const persistedUrlParams = new URLSearchParams();
	['rhf', 's', 'hs', 'show-system-apps'].forEach((k) => {
		urlParams.getAll(k).forEach((v) => {
			persistedUrlParams.append(k, v);
		});
	});
	const search = '?' + persistedUrlParams.toString();

	const scrollContainerRef = React.useRef<HTMLElement>();
	const paddingTopRef = React.useRef<HTMLElement>();
	const paddingBottomRef = React.useRef<HTMLElement>();
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

			dispatch([
				{ type: ActionType.SET_START_INDEX, index: state.visibleIndexTop },
				{ type: ActionType.SET_LENGTH, length: state.visibleLength }
			]);
		});
	}, [windowedListState]);

	// initialize WindowedListState
	React.useLayoutEffect(() => {
		const scrollContainerNode = scrollContainerRef.current;
		if (scrollContainerNode) {
			const rect = scrollContainerNode.getBoundingClientRect();
			windowedListState.viewportHeight = rect.height + windowingThresholdTop + windowingThresholdBottom;
		}
		windowedListState.length = apps.length;
		windowedListState.defaultHeight = 50;
		windowedListState.calculateVisibleIndices();
	}, [apps.length, windowedListState, windowingThresholdTop, windowingThresholdBottom]);

	// pagination
	React.useEffect(() => {
		if (nextPageToken && startIndex + length >= apps.length) {
			return fetchNextPage(nextPageToken);
		}
		return () => {};
	}, [fetchNextPage, apps.length, length, nextPageToken, startIndex]);

	const appRoute = React.useCallback(
		(app: App) => {
			const path = `/${app.getName()}`; // e.g. /apps/48a2d322-5cfe-4323-8823-4dad4528c090
			return {
				path,
				search,
				displayName: app.getDisplayName(), // e.g. controller
				selected: location.pathname === path
			};
		},
		[location.pathname, search]
	);

	return (
		<>
			<Box margin={{ bottom: 'xsmall', left: 'xsmall', right: 'xsmall' }} flex={false}>
				<TextInput
					type="search"
					placeholder="Filter apps..."
					value={filterText}
					onChange={handleFilterTextChange}
					onBlur={flushFilterText}
				/>
			</Box>

			<List ref={scrollContainerRef as any}>
				{isLoading ? <Loading /> : null}

				<Box tag="li" ref={paddingTopRef as any} style={{ height: windowedListState.paddingTop }} flex={false}>
					&nbsp;
				</Box>

				<WindowedList state={windowedListState} thresholdTop={windowingThresholdTop}>
					{(windowedListItemProps) => {
						return apps.map((app, index) => {
							if (index < startIndex) return null;
							const r = appRoute(app);
							return (
								<WindowedListItem key={r.path} index={index} {...windowedListItemProps}>
									{(ref) => (
										<Box
											tag="li"
											ref={ref as any}
											direction="row"
											justify="between"
											align="center"
											basis="auto"
											background={r.selected ? 'brand' : undefined}
										>
											<StyledNavAnchor path={r.path} search={search} selected={r.selected}>
												<Box pad={{ horizontal: 'medium', vertical: 'small' }} fill>
													{r.displayName}
												</Box>
											</StyledNavAnchor>
										</Box>
									)}
								</WindowedListItem>
							);
						});
					}}
				</WindowedList>

				<Box tag="li" ref={paddingBottomRef as any} style={{ height: windowedListState.paddingBottom }} flex={false}>
					&nbsp;
				</Box>
			</List>
		</>
	);
}
