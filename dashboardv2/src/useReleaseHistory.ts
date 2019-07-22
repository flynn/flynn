import * as React from 'react';
import useClient from './useClient';
import useMergeDispatch from './useMergeDispatch';
import {
	Client,
	RequestModifier,
	setNameFilters,
	setStreamUpdates,
	setStreamCreates,
	setPageToken,
	setPageSize,
	StreamReleaseHistoryResponse,
	ReleaseHistoryItem,
	CancelFunc
} from './client';
import { StreamScalesRequest, StreamDeploymentsRequest } from './generated/controller_pb';

export enum ActionType {
	SET_ITEMS = 'useReleaseHistory__SET_ITEMS',
	SET_ERROR = 'useReleaseHistory__SET_ERROR',
	SET_LOADING = 'useReleaseHistory__SET_LOADING',
	SET_NEXT_PAGE_TOKEN = 'useReleaseHistory__SET_NEXT_PAGE_TOKEN',
	SET_NEXT_PAGE_LOADING = 'useReleaseHistory__SET_NEXT_PAGE_LOADING',
	PUSH_PAGE = 'useReleaseHistory__PUSH_PAGE',
	SET_FETCH_NEXT_PAGE = 'useReleaseHistory__SET_FETCH_NEXT_PAGE'
}

type FetchNextPageFunction = (token: NextPageTokens | null) => CancelFunc;

export class NextPageTokens {
	public scales: string;
	public deployments: string;

	constructor(scales: string, deployments: string) {
		this.scales = scales;
		this.deployments = deployments;
	}

	toString() {
		return this.scales + this.deployments;
	}
}

function fetchNextPageFactory(
	client: Client,
	appName: string,
	scaleReqModifiers: RequestModifier<StreamScalesRequest>[],
	deploymentReqModifiers: RequestModifier<StreamDeploymentsRequest>[],
	pagesMap: PagesMap,
	dispatch: Dispatcher
) {
	return (pageTokens: NextPageTokens | null) => {
		let cancel = () => {};

		if (pageTokens === null) return cancel;
		if (pagesMap.has(pageTokens)) return cancel;

		// initialize page so additional calls with the same token will be void
		// (see above).
		pagesMap.set(pageTokens, []);

		const scaleRequestsNextPageToken = pageTokens.scales;
		const deploymentsNextPageToken = pageTokens.deployments;

		dispatch({ type: ActionType.SET_NEXT_PAGE_LOADING, loading: true });

		cancel = client.streamReleaseHistory(
			(res: StreamReleaseHistoryResponse, error: Error | null) => {
				if (error) {
					dispatch({ type: ActionType.SET_NEXT_PAGE_LOADING, loading: false });
					return;
				}

				// wait for both streams to have a response
				if (!res.isComplete()) return;

				dispatch([
					{
						type: ActionType.SET_NEXT_PAGE_TOKEN,
						token: new NextPageTokens(res.getScaleRequestsNextPageToken(), res.getDeploymentsNextPageToken())
					},
					{ type: ActionType.PUSH_PAGE, token: pageTokens, items: res.getItemsList() },
					{ type: ActionType.SET_NEXT_PAGE_LOADING, loading: false }
				]);
			},
			// scale request modifiers
			scaleRequestsNextPageToken
				? [setNameFilters(appName), setPageToken(scaleRequestsNextPageToken), ...scaleReqModifiers]
				: null,
			// deployment request modifiers
			deploymentsNextPageToken
				? [setNameFilters(appName), setPageToken(deploymentsNextPageToken), ...deploymentReqModifiers]
				: null
		);
		return cancel;
	};
}

interface SetItemsAction {
	type: ActionType.SET_ITEMS;
	items: ReleaseHistoryItem[];
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error | null;
}

interface SetLoadingAction {
	type: ActionType.SET_LOADING;
	loading: boolean;
}

interface SetNextPageTokenAction {
	type: ActionType.SET_NEXT_PAGE_TOKEN;
	token: NextPageTokens;
}

interface SetNextPageLoadingAction {
	type: ActionType.SET_NEXT_PAGE_LOADING;
	loading: boolean;
}

interface PushPageAction {
	type: ActionType.PUSH_PAGE;
	token: NextPageTokens;
	items: ReleaseHistoryItem[];
}

interface SetFetchNextPageAction {
	type: ActionType.SET_FETCH_NEXT_PAGE;
	fetchNextPage: FetchNextPageFunction;
}

export type Action =
	| SetItemsAction
	| SetErrorAction
	| SetLoadingAction
	| SetNextPageTokenAction
	| SetNextPageLoadingAction
	| PushPageAction
	| SetFetchNextPageAction;

type Dispatcher = (actions: Action | Action[]) => void;

type PagesMap = Map<NextPageTokens, ReleaseHistoryItem[]>;

export interface State {
	items: ReleaseHistoryItem[];
	allItems: ReleaseHistoryItem[];
	loading: boolean;
	error: Error | null;
	nextPageToken: NextPageTokens | null;
	nextPageLoading: boolean;
	pagesMap: PagesMap;
	pageOrder: NextPageTokens[];
	fetchNextPage: FetchNextPageFunction;
}

export function initialState(): State {
	return {
		items: [],
		allItems: [],
		loading: true,
		error: null,
		nextPageToken: null,
		nextPageLoading: false,
		pagesMap: new Map([]) as PagesMap,
		pageOrder: [] as NextPageTokens[],
		fetchNextPage: (tokens: NextPageTokens | null) => () => {}
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}

	function buildAllItems(items: ReleaseHistoryItem[], pagesMap: PagesMap, pageOrder: NextPageTokens[]) {
		return items.concat(
			pageOrder.reduce((m: ReleaseHistoryItem[], pts: NextPageTokens) => {
				return m.concat(pagesMap.get(pts) || []);
			}, [])
		);
	}

	return actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_ITEMS:
				nextState.items = action.items;
				nextState.allItems = buildAllItems(action.items, prevState.pagesMap, prevState.pageOrder);
				return nextState;

			case ActionType.SET_ERROR:
				nextState.error = action.error;
				return nextState;

			case ActionType.SET_LOADING:
				nextState.loading = action.loading;
				return nextState;

			case ActionType.SET_NEXT_PAGE_TOKEN:
				nextState.nextPageToken = action.token;
				return nextState;

			case ActionType.SET_NEXT_PAGE_LOADING:
				nextState.nextPageLoading = action.loading;
				return nextState;

			case ActionType.PUSH_PAGE:
				nextState.pagesMap.set(action.token, action.items);
				nextState.pageOrder = prevState.pageOrder.concat([action.token]);
				nextState.allItems = buildAllItems(prevState.items, nextState.pagesMap, nextState.pageOrder);
				return nextState;

			case ActionType.SET_FETCH_NEXT_PAGE:
				nextState.fetchNextPage = action.fetchNextPage;
				return nextState;

			default:
				return prevState;
		}
	}, prevState);
}

const emptyScaleReqModifiersArray = [] as RequestModifier<StreamScalesRequest>[];
const emptyDeploymentReqModifiersArray = [] as RequestModifier<StreamDeploymentsRequest>[];

export function useReleaseHistoryWithDispatch(
	appName: string,
	scaleReqModifiers: RequestModifier<StreamScalesRequest>[],
	deploymentReqModifiers: RequestModifier<StreamDeploymentsRequest>[],
	scalesEnabled: boolean = false,
	deploymentsEnabled: boolean = false,
	callerDispatch: Dispatcher
) {
	const client = useClient();
	const [{ pagesMap }, localDispatch] = React.useReducer(reducer, initialState());
	const dispatch = useMergeDispatch(localDispatch, callerDispatch, false);
	if (scaleReqModifiers.length === 0) {
		scaleReqModifiers = emptyScaleReqModifiersArray;
	}
	if (deploymentReqModifiers.length === 0) {
		deploymentReqModifiers = emptyDeploymentReqModifiersArray;
	}
	React.useEffect(() => {
		const fetchNextPage = fetchNextPageFactory(
			client,
			appName,
			scaleReqModifiers,
			deploymentReqModifiers,
			pagesMap,
			dispatch
		);
		dispatch({ type: ActionType.SET_FETCH_NEXT_PAGE, fetchNextPage });
	}, [client, appName, scaleReqModifiers, deploymentReqModifiers, pagesMap, dispatch]);
	React.useEffect(() => {
		if (!scalesEnabled && !deploymentsEnabled) {
			return;
		}

		const cancel = client.streamReleaseHistory(
			(res: StreamReleaseHistoryResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}

				// wait for both streams to have a response
				if (!res.isComplete()) return;

				dispatch([
					{ type: ActionType.SET_ITEMS, items: res.getItemsList() },
					{
						type: ActionType.SET_NEXT_PAGE_TOKEN,
						token: new NextPageTokens(res.getScaleRequestsNextPageToken(), res.getDeploymentsNextPageToken())
					},
					{ type: ActionType.SET_ERROR, error },
					{ type: ActionType.SET_LOADING, loading: false }
				]);
			},
			// scale request modifiers
			[setNameFilters(appName), setPageSize(50), setStreamUpdates(), setStreamCreates(), ...scaleReqModifiers],
			// deployment request modifiers
			[setNameFilters(appName), setPageSize(50), setStreamUpdates(), setStreamCreates(), ...deploymentReqModifiers]
		);
		return cancel;
	}, [client, appName, scaleReqModifiers, deploymentReqModifiers, scalesEnabled, deploymentsEnabled, dispatch]);
}
