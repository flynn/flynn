import * as React from 'react';
import useClient from './useClient';
import { App, StreamAppsRequest, StreamAppsResponse } from './generated/controller_pb';
import { RequestModifier, setStreamCreates, setStreamUpdates, setPageToken, setPageSize } from './client';

const emptyReqModifiersArray = [] as RequestModifier<StreamAppsRequest>[];

export default function useAppsList(reqModifiers: RequestModifier<StreamAppsRequest>[] = []) {
	const client = useClient();
	const [appsLoading, setAppsLoading] = React.useState(true);
	const [apps, setApps] = React.useState<App[]>([]);
	const [error, setError] = React.useState<Error | null>(null);
	const [nextPageToken, setNextPageToken] = React.useState('');
	const pagesMap = React.useMemo(() => new Map<string, App[]>(), []);
	const [pageOrder, setPageOrder] = React.useState<string[]>([]);
	if (reqModifiers.length === 0) {
		reqModifiers = emptyReqModifiersArray;
	}
	React.useEffect(() => {
		setAppsLoading(true);
		setApps([]);
		const cancel = client.streamApps(
			(res: StreamAppsResponse, error: Error | null) => {
				if (error) {
					setError(error);
					setAppsLoading(false);
					return;
				}
				setApps(res.getAppsList());
				setNextPageToken(res.getNextPageToken());
				setError(null);
				setAppsLoading(false);
			},
			setPageSize(50),
			setStreamCreates(),
			setStreamUpdates(),
			...reqModifiers
		);
		return cancel;
	}, [client, reqModifiers]);

	const fetchNextPage = React.useCallback(
		(pageToken) => {
			let cancel = () => {};

			if (pageToken === '') return cancel;
			if (pagesMap.has(pageToken)) return cancel;

			// initialize page so additional calls with the same token will be void
			// (see above).
			pagesMap.set(pageToken, []);

			cancel = client.streamApps(
				(res: StreamAppsResponse, error: Error | null) => {
					if (error) {
						setError(error);
						setAppsLoading(false);
						return;
					}
					pagesMap.set(pageToken, res.getAppsList());
					setNextPageToken(res.getNextPageToken());
					setPageOrder(pageOrder.concat([pageToken]));
					setAppsLoading(false);
				},
				setPageToken(pageToken),
				...reqModifiers
			);
			return cancel;
		},
		[client, pageOrder, pagesMap, reqModifiers]
	);

	const allApps = React.useMemo(() => {
		return apps.concat(
			pageOrder.reduce((m: App[], pts: string) => {
				return m.concat(pagesMap.get(pts) || []);
			}, [])
		);
	}, [apps, pageOrder, pagesMap]);

	return {
		loading: appsLoading,
		apps: allApps,
		nextPageToken,
		fetchNextPage,
		error
	};
}
