import * as React from 'react';
import { debounce } from 'lodash';

export interface Props {
	children: React.ReactNode;
	timeoutMs?: number;
}

export default function Debounced({ children, timeoutMs = 0 }: Props) {
	const [shouldRender, setShouldRender] = React.useState(false);
	React.useEffect(() => {
		const fn = debounce(() => setShouldRender(true), timeoutMs);
		fn();
		return fn.cancel;
	}, [timeoutMs]);

	if (shouldRender) {
		return <>{children}</>;
	}
	return null;
}
