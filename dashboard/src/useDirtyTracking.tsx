import * as React from 'react';
import arrayToFormattedString from './util/arrayToFormattedString';
import Notification from './Notification';

const state = {
	dirtyKeys: new Set<string>()
};

export interface ContextObject {
	key: string;
	dirty: boolean;
}

export const Context = React.createContext<ContextObject>(buildContext('default'));

export function buildContext(key: string): ContextObject {
	return { key, dirty: false };
}

function otherDirtyKeys(ctx: ContextObject): string[] {
	return Array.from(state.dirtyKeys)
		.sort((a, b) => a.localeCompare(b))
		.filter((k) => k !== ctx.key);
}

function otherDirtyKeysFormatted(ctx: ContextObject): [string, number] {
	const dirtyKeys = otherDirtyKeys(ctx);
	if (dirtyKeys.length === 0) return ['', 0];
	return [arrayToFormattedString(dirtyKeys), dirtyKeys.length];
}

interface DirtyNotificationProps {}

export const DirtyNotification = (props: DirtyNotificationProps): ReturnType<React.FC> => {
	const ctx = React.useContext(Context);
	const [dirtySectionNames, n] = React.useMemo(() => otherDirtyKeysFormatted(ctx), [ctx]);
	if (n === 0) return null;
	return (
		<Notification
			message={`NOTE: Changes made to the following section${
				n > 1 ? 's' : ''
			} will need to be submitted separately: ${dirtySectionNames}`}
			status="warning"
			margin="small"
		/>
	);
};

export default function useDirtyTracking(): [() => void, () => void] {
	const ctx = React.useContext(Context);
	React.useDebugValue(() => `useDirtyTracking key=${ctx.key} dirty=${ctx.dirty}`);
	const setDirty = React.useCallback(() => {
		state.dirtyKeys.add(ctx.key);
		ctx.dirty = true;
	}, [ctx]);
	const unsetDirty = React.useCallback(() => {
		state.dirtyKeys.delete(ctx.key);
		ctx.dirty = false;
	}, [ctx]);
	React.useEffect(() => {
		// call unsetDirty when component unmounted
		return unsetDirty();
	}, [unsetDirty]);
	return [setDirty, unsetDirty];
}
