import * as React from 'react';
import { useHistory, useLocation, useRouteMatch, match as MatchObject } from 'react-router-dom';
import * as H from 'history';
import { Omit } from 'grommet/utils';
import { navProtectionEnabled, confirmNavigation, getProtectedParams } from './useNavProtection';

type HistoryPathMutationFn<HistoryLocationState> = (path: H.Path, state?: HistoryLocationState) => void;
type HistoryLocationMutationFn<HistoryLocationState> = (
	location: H.LocationDescriptorObject<HistoryLocationState>
) => void;

interface History<HistoryLocationState> extends Omit<H.History, 'push' | 'replace'> {
	push(path: H.Path, state?: HistoryLocationState): boolean;
	push(location: H.LocationDescriptorObject<HistoryLocationState>): boolean;
	replace(path: H.Path, state?: HistoryLocationState): boolean;
	replace(location: H.LocationDescriptorObject<HistoryLocationState>): boolean;
}

export interface UseRouterObejct<TParams> {
	urlParams: URLSearchParams;
	history: History<H.LocationState>;
	location: H.LocationDescriptorObject;
	match: MatchObject<TParams>;
}

function urlForPath(path: string): URL {
	const a = document.createElement('a');
	a.href = path;
	return new URL(a.href);
}

export default function useRouter<TParams = {}>(): UseRouterObejct<TParams> {
	const _history = useHistory();
	const location = useLocation();
	const match = useRouteMatch<TParams>();
	const urlParams = React.useMemo(() => {
		return new URLSearchParams(location.search);
	}, [location.search]);

	// Checks if the next route will interfear with a protected component and
	// confirms before navigating if that's the case
	const handlePushOrReplace = React.useCallback(
		(
			fn: HistoryPathMutationFn<H.LocationState> | HistoryLocationMutationFn<H.LocationState>,
			pathOrLocation: H.Path | H.LocationDescriptorObject,
			state?: H.LocationState
		): boolean => {
			let passState = false;
			let path = '' as string;
			let pathname = '' as string;
			let nextUrlParams;
			const prevUrlParams = urlParams;
			if (typeof pathOrLocation === 'string') {
				passState = true;
				path = pathOrLocation;
				pathname = urlForPath(path).pathname;
				nextUrlParams = new URLSearchParams(path.split('?')[1] || '');
			} else {
				path = pathOrLocation.pathname || '';
				pathname = path;
				nextUrlParams = new URLSearchParams(location.search);
			}

			let protectedParamsChanged = false;
			for (let [pk, pv] of getProtectedParams()) {
				if (nextUrlParams.getAll(pk).includes(pv)) {
					protectedParamsChanged = !prevUrlParams.getAll(pk).includes(pv);
				} else if (prevUrlParams.getAll(pk).includes(pv)) {
					protectedParamsChanged = true;
				}
				if (protectedParamsChanged) break;
			}

			if (
				navProtectionEnabled() &&
				(pathname !== location.pathname || protectedParamsChanged) &&
				!confirmNavigation()
			) {
				// let the caller know navigation was canceled
				return false;
			}
			if (passState) {
				(fn as HistoryPathMutationFn<H.LocationState>)(pathOrLocation as string, state);
			} else {
				(fn as HistoryLocationMutationFn<H.LocationState>)(pathOrLocation as H.LocationDescriptorObject);
			}
			// let the caller know navigation has proceeded
			return true;
		},
		[location.pathname, location.search, urlParams]
	);

	// Override the history object's push and replace to insert navigation
	// protection layer
	const history = React.useMemo(() => {
		return Object.assign({}, _history, {
			push: (pathOrLocation: H.Path | H.LocationDescriptorObject, state?: H.LocationState): boolean => {
				return handlePushOrReplace(_history.push.bind(_history), pathOrLocation, state);
			},
			replace: (pathOrLocation: H.Path | H.LocationDescriptorObject, state?: H.LocationState): boolean => {
				return handlePushOrReplace(_history.replace.bind(_history), pathOrLocation, state);
			}
		});
	}, [_history, handlePushOrReplace]);

	return {
		urlParams,
		history,
		location,
		match
	};
}
