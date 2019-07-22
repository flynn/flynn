import * as React from 'react';

const navProtectionState = {
	enabledKeys: new Set<Symbol>(),
	enabledContexts: new Set([]) as Set<NavProtectionContextObject>
};

const CONFIRM_MESSAGE =
	'This page is asking you to confirm that you want to leave - data you have entered may not be saved.';

function handleWindowBeforeUnload(e: any) {
	if (!navProtectionEnabled()) return;

	// See https://developer.mozilla.org/en-US/docs/Web/API/WindowEventHandlers/onbeforeunload
	// Cancel the event
	e.preventDefault();
	// Chrome requires returnValue to be set
	e.returnValue = '';
}

function handleNavProtectionEnabled() {
	window.addEventListener('beforeunload', handleWindowBeforeUnload);
}

function handleNavProtectionDisabled() {
	window.removeEventListener('beforeunload', handleWindowBeforeUnload);
}

export interface NavProtectionContextObject {
	params: URLSearchParams;
}

export const NavProtectionContext = React.createContext<NavProtectionContextObject | null>(null);

export function buildNavProtectionContext(params: string): NavProtectionContextObject {
	return { params: new URLSearchParams(params) };
}

export function confirmNavigation(): boolean {
	return window.confirm(CONFIRM_MESSAGE);
}

export function navProtectionEnabled(): boolean {
	return navProtectionState.enabledKeys.size > 0;
}

export function getProtectedParams(): URLSearchParams {
	const params = new URLSearchParams();
	for (let ctx of navProtectionState.enabledContexts) {
		for (let [k, v] of ctx.params) {
			params.append(k, v);
		}
	}
	return params;
}

let debugIndex = 0;
export default function useNavProtection(): [() => void, () => void] {
	const navProtectionContext = React.useContext(NavProtectionContext);
	const [navProtectionKey] = React.useState(() => Symbol(`useNavProtection key(${debugIndex++})`));
	const enableNavProtection = React.useCallback(() => {
		const prevEnabled = navProtectionEnabled();
		navProtectionState.enabledKeys.add(navProtectionKey);
		if (navProtectionContext) {
			navProtectionState.enabledContexts.add(navProtectionContext);
		}
		if (!prevEnabled) {
			handleNavProtectionEnabled();
		}
	}, [navProtectionContext, navProtectionKey]);
	const disableNavProtection = React.useCallback(() => {
		const prevEnabled = navProtectionEnabled();
		navProtectionState.enabledKeys.delete(navProtectionKey);
		if (navProtectionContext) {
			navProtectionState.enabledContexts.delete(navProtectionContext);
		}
		if (prevEnabled && !navProtectionEnabled()) {
			handleNavProtectionDisabled();
		}
	}, [navProtectionContext, navProtectionKey]);
	React.useEffect(() => {
		// call disableNavProtection when component unmounted
		return disableNavProtection;
	}, [disableNavProtection]);
	return [enableNavProtection, disableNavProtection];
}
