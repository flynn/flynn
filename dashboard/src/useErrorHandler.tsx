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

function isCancelableError(error: Error): error is CancelableError {
	if (error.hasOwnProperty('cancel') && typeof (error as CancelableError).cancel === 'function') {
		return true;
	}
	return false;
}

export interface RetriableError extends Error {
	retry: () => void;
	key: Symbol;
}

function isRetriableError(error: Error): error is RetriableError {
	if (error.hasOwnProperty('retry') && typeof (error as RetriableError).retry === 'function') {
		return true;
	}
	return false;
}

const callbacks = new Set<() => void>();

const errors = new Map<Symbol, Array<CancelableError | RetriableError>>();

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
	let wrappedError: CancelableError | RetriableError;
	const cancel = () => {
		const arr = errors.get(key);
		if (!arr) return;
		const index = arr.indexOf(wrappedError);
		if (index === -1) return;
		errors.set(key, arr.slice(0, index).concat(arr.slice(index + 1)));
		for (let fn of callbacks) {
			fn();
		}
	};
	if (typeof (error as any).retry === 'function') {
		wrappedError = Object.assign(new Error(error.message), error, {
			key,
			retry: () => {
				cancel();
				return (error as any).retry();
			}
		});
	} else {
		wrappedError = Object.assign(new Error(error.message), error, {
			key: key,
			cancel
		});
	}

	errors.set(key, (errors.get(key) || []).concat(wrappedError));
	for (let fn of callbacks) {
		fn();
	}
	return cancel;
}

export function useErrors(): Array<CancelableError | RetriableError> {
	const [errorsArr, setErrors] = React.useState<Array<CancelableError | RetriableError>>([]);
	React.useEffect(() => {
		return registerCallback(() => {
			const arr = [] as Array<CancelableError | RetriableError>;
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
			{errors.map((error: CancelableError | RetriableError, index: number) => {
				let retry = undefined;
				if (isRetriableError(error)) {
					retry = () => error.retry();
				}
				let cancel = undefined;
				if (isCancelableError(error)) {
					cancel = () => error.cancel();
				}
				return (
					<Notification
						key={error.key.toString() + index}
						message={error.message}
						status="warning"
						onClose={cancel}
						onRetryClick={retry}
						margin="small"
					/>
				);
			})}
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
