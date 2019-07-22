import * as React from 'react';
import isActionType from './util/isActionType';

import { ActionType as AppActionType, Action as AppAction } from './useApp';
import { ActionType as AppReleaseActionType, Action as AppReleaseAction } from './useAppRelease';
import { ActionType as AppScaleActionType, Action as AppScaleAction } from './useAppScale';
import { ActionType as ReleaseActionType, Action as ReleaseAction } from './useRelease';
import { ActionType as ReleaseHistoryActionType, Action as ReleaseHistoryAction } from './useReleaseHistory';

type Dispatcher<Action> = (actions: Action | Action[]) => void;

export default function useMergeDispatch<Action>(
	localDispatch: Dispatcher<Action>,
	callerDispatch: Dispatcher<Action>,
	filterUseX: boolean = true
) {
	const dispatch = React.useCallback(
		(actions: Action | Action[]) => {
			if (!Array.isArray(actions)) actions = [actions];
			localDispatch(actions);

			// don't call callerDispatch with any useX actions
			if (filterUseX) {
				actions = actions.filter((action) => {
					if (
						isActionType<AppAction>(AppActionType, action) ||
						isActionType<AppReleaseAction>(AppReleaseActionType, action) ||
						isActionType<AppScaleAction>(AppScaleActionType, action) ||
						isActionType<ReleaseAction>(ReleaseActionType, action) ||
						isActionType<ReleaseHistoryAction>(ReleaseHistoryActionType, action)
					) {
						// TODO(jvatic): useAppsList
						return false;
					}
					return true;
				});
			}
			callerDispatch(actions);
		},
		[] // eslint-disable-line react-hooks/exhaustive-deps
	);
	return dispatch;
}
