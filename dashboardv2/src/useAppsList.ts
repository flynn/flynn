import * as React from 'react';
import useClient from './useClient';
import { Client, CancelFunc } from './client';
import { App, StreamAppsRequest, StreamAppsResponse } from './generated/controller_pb';
import { RequestModifier, setStreamCreates, setStreamUpdates, setPageToken, setPageSize } from './client';

export enum ActionType {
	SET_APPS = 'useAppsList__SET_APPS',
	SET_PAGE = 'useAppsList__SET_PAGE',
	SET_NEXT_PAGE_TOKEN = 'useAppsList__SET_NEXT_PAGE_TOKEN',
	SET_FETCH_NEXT_PAGE = 'useAppsList__SET_FETCH_NEXT_PAGE',
	SET_ERROR = 'useAppsList__SET_ERROR',
	SET_LOADING = 'useAppsList__SET_LOADING'
}

interface SetAppsAction {
	type: ActionType.SET_APPS;
	apps: App[];
}

interface SetPageAction {
	type: ActionType.SET_PAGE;
	apps: App[];
	token: string;
}

interface SetNextPageTokenAction {
	type: ActionType.SET_NEXT_PAGE_TOKEN;
	token: string;
}

interface SetFetchNextPageAction {
	type: ActionType.SET_FETCH_NEXT_PAGE;
	fetchNextPage: FetchNextPageFunction;
}

interface SetErrorAction {
	type: ActionType.SET_ERROR;
	error: Error | null;
}

interface SetLoadingAction {
	type: ActionType.SET_LOADING;
	loading: boolean;
}

export type Action =
	| SetAppsAction
	| SetPageAction
	| SetNextPageTokenAction
	| SetFetchNextPageAction
	| SetErrorAction
	| SetLoadingAction;

type Dispatcher = (actions: Action | Action[]) => void;

export interface State {
	apps: App[];
	allApps: App[];
	pagesMap: Map<string, App[]>;
	pageOrder: string[]; // array of page tokens
	nextPageToken: string;
	fetchNextPage: FetchNextPageFunction;
	loading: boolean;
	error: Error | null;
}

export function initialState(): State {
	return {
		apps: [],
		allApps: [],
		pagesMap: new Map<string, App[]>(),
		pageOrder: [],
		nextPageToken: '',
		fetchNextPage: () => {
			return () => {};
		},
		loading: true,
		error: null
	};
}

type Reducer = (prevState: State, actions: Action | Action[]) => State;

export function reducer(prevState: State, actions: Action | Action[]): State {
	if (!Array.isArray(actions)) {
		actions = [actions];
	}
	const nextState = actions.reduce((prevState: State, action: Action) => {
		const nextState = Object.assign({}, prevState);
		switch (action.type) {
			case ActionType.SET_APPS:
				nextState.apps = action.apps;
				return nextState;

			case ActionType.SET_PAGE:
				nextState.pagesMap = new Map(prevState.pagesMap);
				nextState.pagesMap.set(action.token, action.apps);
				nextState.pageOrder = prevState.pageOrder.concat([action.token]);
				return nextState;

			case ActionType.SET_NEXT_PAGE_TOKEN:
				nextState.nextPageToken = action.token;
				return nextState;

			case ActionType.SET_FETCH_NEXT_PAGE:
				nextState.fetchNextPage = action.fetchNextPage;
				return nextState;

			case ActionType.SET_LOADING:
				nextState.loading = action.loading;
				return nextState;

			case ActionType.SET_ERROR:
				nextState.error = action.error;
				return nextState;

			default:
				return prevState;
		}
	}, prevState);

	if (nextState === prevState) return prevState;

	(() => {
		const { apps, pagesMap, pageOrder } = nextState;
		if (apps === prevState.apps && pagesMap === prevState.pagesMap && pageOrder === prevState.pageOrder) {
			return;
		}

		nextState.allApps = apps.concat(
			pageOrder.reduce((m: App[], pts: string) => {
				return m.concat(pagesMap.get(pts) || []);
			}, [])
		);
	})();

	return nextState;
}

const emptyReqModifiersArray = [] as RequestModifier<StreamAppsRequest>[];

type FetchNextPageFunction = (token: string) => CancelFunc;

function fetchNextPageFactory(
	client: Client,
	dispatch: Dispatcher,
	reqModifiers: RequestModifier<StreamAppsRequest>[]
): FetchNextPageFunction {
	return (pageToken) => {
		let cancel = () => {};
		if (pageToken === '') return cancel;

		cancel = client.streamApps(
			(res: StreamAppsResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_LOADING, loading: false },
						{ type: ActionType.SET_ERROR, error }
					]);
					return;
				}
				dispatch([
					{ type: ActionType.SET_PAGE, token: pageToken, apps: res.getAppsList() },
					{ type: ActionType.SET_NEXT_PAGE_TOKEN, token: res.getNextPageToken() },
					{ type: ActionType.SET_LOADING, loading: false },
					{ type: ActionType.SET_ERROR, error: null }
				]);
			},
			setPageToken(pageToken),
			...reqModifiers
		);
		return cancel;
	};
}

export function useAppsListWithDispatch(dispatch: Dispatcher, reqModifiers: RequestModifier<StreamAppsRequest>[] = []) {
	const client = useClient();
	if (reqModifiers.length === 0) {
		reqModifiers = emptyReqModifiersArray;
	}
	React.useEffect(() => {
		const cancel = client.streamApps(
			(res: StreamAppsResponse, error: Error | null) => {
				if (error) {
					dispatch([
						{ type: ActionType.SET_ERROR, error },
						{ type: ActionType.SET_LOADING, loading: false }
					]);
					return;
				}
				dispatch([
					{ type: ActionType.SET_APPS, apps: res.getAppsList() },
					{ type: ActionType.SET_NEXT_PAGE_TOKEN, token: res.getNextPageToken() },
					{ type: ActionType.SET_ERROR, error: null },
					{ type: ActionType.SET_LOADING, loading: false },
					{ type: ActionType.SET_FETCH_NEXT_PAGE, fetchNextPage: fetchNextPageFactory(client, dispatch, reqModifiers) }
				]);
			},
			setPageSize(50),
			setStreamCreates(),
			setStreamUpdates(),
			...reqModifiers
		);
		return cancel;
	}, [client, reqModifiers, dispatch]);
}
