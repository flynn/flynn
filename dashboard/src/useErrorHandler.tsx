import * as React from 'react';
import { grpc } from '@improbable-eng/grpc-web';
import Notification from './Notification';

type CancelFunc = () => void;

export interface ErrorHandler {
	(error: Error): CancelFunc;
	key: Symbol;
}

export interface CancelableError extends Error {
	cancel: () => void;
	key: Symbol;
}

const callbacks = new Set<() => void>();

const errors = new Map<Symbol, CancelableError[]>();

export function registerCallback(h: () => void): () => void {
	callbacks.add(h);
	return () => {
		callbacks.delete(h);
	};
}

function handleError(error: Error, key: Symbol = Symbol('useErrorHandler key(undefined)')): CancelFunc {
	if ((error as any).code === grpc.Code.Unknown) {
		if (console && typeof console.error === 'function') {
			console.error(error);
		}
		return () => {};
	}
	const cancelableError = Object.assign(new Error(error.message), error, {
		cancel: () => {
			const arr = errors.get(key);
			if (!arr) return;
			const index = arr.indexOf(cancelableError);
			if (index === -1) return;
			errors.set(key, arr.slice(0, index).concat(arr.slice(index + 1)));
			for (let fn of callbacks) {
				fn();
			}
		},
		key: key
	});
	errors.set(key, (errors.get(key) || []).concat(cancelableError));
	for (let fn of callbacks) {
		fn();
	}
	return cancelableError.cancel;
}

export function useErrors(): CancelableError[] {
	const [errorsArr, setErrors] = React.useState<CancelableError[]>([]);
	React.useEffect(() => {
		return registerCallback(() => {
			const arr = [] as CancelableError[];
			for (let v of errors.values()) {
				arr.push(...v);
			}
			setErrors(arr);
		});
	}, []);
	return errorsArr;
}

export function DisplayErrors() {
	const errors = useErrors();
	return (
		<>
			{errors.map((error: CancelableError, index: number) => (
				<Notification
					key={error.key.toString() + index}
					message={error.message}
					status="warning"
					onClose={() => error.cancel()}
					margin="small"
				/>
			))}
		</>
	);
}

let debugIndex = 0;
export function handleErrorFactory(): ErrorHandler {
	const key = Symbol(`useErrorHandler key(${debugIndex++})`);
	return Object.assign(
		(error: Error) => {
			return handleError(error, key);
		},
		{ key }
	);
}

export default function useErrorHandler(): ErrorHandler {
	const fn = React.useMemo(() => handleErrorFactory(), []);
	return fn;
}
