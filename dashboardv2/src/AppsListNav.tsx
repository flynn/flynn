import * as React from 'react';
import { Box } from 'grommet';

import useRouter from './useRouter';
import useAppsList from './useAppsList';
import useErrorHandler from './useErrorHandler';

import { App } from './generated/controller_pb';
import { excludeAppsWithLabels } from './client';

import Loading from './Loading';
import NavAnchor from './NavAnchor';
import WindowedListState from './WindowedListState';
import WindowedList, { WindowedListItem } from './WindowedList';

export interface Props {
	onNav?: (path: string) => void;
}

export default function AppsListNav({ onNav }: Props) {
	const { location, urlParams } = useRouter();
	const excludeSystemAppsFilter = React.useMemo(() => excludeAppsWithLabels([['flynn-system-app', 'true']]), []);
	const showSystemApps = urlParams.get('show-system-apps') === 'true';
	const appsListFilters = React.useMemo(() => (showSystemApps ? [] : [excludeSystemAppsFilter]), [
		excludeSystemAppsFilter,
		showSystemApps
	]);
	const { apps, nextPageToken, fetchNextPage, loading: isLoading, error: appsError } = useAppsList(appsListFilters);
	const handleError = useErrorHandler();
	React.useEffect(() => {
		let cancel = () => {};
		if (appsError) {
			cancel = handleError(appsError);
		}
		return cancel;
	}, [appsError, handleError]);

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
	const [startIndex, setStartIndex] = React.useState(0);
	const [length, setLength] = React.useState(0);
	const windowedListState = React.useMemo(() => new WindowedListState(), []);
	const windowingThresholdTop = 600;
	const windowingThresholdBottom = 600;
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

			setStartIndex(state.visibleIndexTop);
			setLength(state.visibleLength);
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
	}, [apps.length, windowedListState]);

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

	const navHandler = React.useCallback(
		(path: string) => {
			if (location.pathname === path) {
				return;
			}
			if (onNav) {
				onNav(path);
			}
		},
		[location.pathname, onNav]
	);

	return (
		<Box
			ref={scrollContainerRef as any}
			tag="ul"
			margin="none"
			pad="none"
			flex={true}
			overflow={{ vertical: 'auto', horizontal: 'auto' }}
		>
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
									<NavAnchor path={r.path} search={search} onNav={navHandler}>
										<Box
											tag="li"
											ref={ref as any}
											direction="row"
											justify="between"
											align="center"
											border="bottom"
											pad={{ horizontal: 'medium', vertical: 'small' }}
											basis="auto"
											flex={false}
											background={r.selected ? 'accent-1' : 'neutral-1'}
										>
											{r.displayName}
										</Box>
									</NavAnchor>
								)}
							</WindowedListItem>
						);
					});
				}}
			</WindowedList>

			<Box tag="li" ref={paddingBottomRef as any} style={{ height: windowedListState.paddingBottom }} flex={false}>
				&nbsp;
			</Box>
		</Box>
	);
}
