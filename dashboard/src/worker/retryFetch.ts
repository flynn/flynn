import _debug from './debug';

function debug(msg: string, ...args: any[]) {
	_debug(`[retryFetch]: ${msg}`, ...args);
}

const maxRetries = 3;
export default async function retryFetch(
	request: string | Request,
	init?: RequestInit,
	nRetries: number = 0
): Promise<Response> {
	const response = await fetch(request, init);
	if (response.ok) return response;
	if (isRetriableStatus(response.status) && nRetries < maxRetries) {
		const signal = (init && init.signal) || null;
		return new Promise<Response>((resolve, reject) => {
			const timeout = calcTimeout(nRetries);
			debug(`retrying fetch in ${timeout}ms`);
			let complete = false;
			const timeoutID = setTimeout(() => {
				retryFetch(request, init, nRetries + 1)
					.then((response: Response) => {
						if (complete) return;
						complete = true;
						resolve(response);
					})
					.catch((error) => {
						if (complete) return;
						complete = true;
						reject(error);
					});
			}, timeout);
			if (signal) {
				signal.addEventListener('abort', () => {
					clearTimeout(timeoutID);
					if (complete) return;
					complete = true;
					reject(new Error('AbortError'));
				});
			}
		});
	}
	return response;
}

function isRetriableStatus(status: number): boolean {
	if (status === 0) return true;
	if (status <= 500 && status < 600) return true;
	return false;
}

function calcTimeout(n: number): number {
	let timeout = 1000;
	for (let i = 0; i < n; i++) {
		timeout += timeout;
	}
	return timeout;
}
