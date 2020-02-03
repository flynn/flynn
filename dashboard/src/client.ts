import { grpc } from '@improbable-eng/grpc-web';
import { Timestamp } from 'google-protobuf/google/protobuf/timestamp_pb';
import { BrowserHeaders } from 'browser-headers';
import * as timestamp_pb from 'google-protobuf/google/protobuf/timestamp_pb';

import Config, { PrivateConfig } from './config';
import { ControllerClient, ServiceError, Status, ResponseStream } from './generated/controller_pb_service';
import {
	StreamAppsRequest,
	StreamAppsResponse,
	UpdateAppRequest,
	App,
	StreamReleasesRequest,
	StreamReleasesResponse,
	CreateReleaseRequest,
	Release,
	ReleaseTypeMap,
	ScaleRequest,
	StreamScalesRequest,
	StreamScalesResponse,
	ScaleRequestStateMap,
	CreateScaleRequest,
	CreateDeploymentRequest,
	ExpandedDeployment,
	StreamDeploymentsRequest,
	StreamDeploymentsResponse,
	DeploymentStatusMap,
	LabelFilter
} from './generated/controller_pb';

export interface Client {
	// auth
	login: (token: string, cb: AuthCallback) => CancelFunc;
	logout: (cb: AuthCallback) => CancelFunc;

	// read API
	streamApps: (cb: AppsCallback, ...reqModifiers: RequestModifier<StreamAppsRequest>[]) => CancelFunc;
	streamReleases: (cb: ReleasesCallback, ...reqModifiers: RequestModifier<StreamReleasesRequest>[]) => CancelFunc;
	streamScales: (cb: ScaleRequestsCallback, ...reqModifiers: RequestModifier<StreamScalesRequest>[]) => CancelFunc;
	streamDeployments: (
		cb: DeploymentsCallback,
		...reqModifiers: RequestModifier<StreamDeploymentsRequest>[]
	) => CancelFunc;
	streamReleaseHistory: (
		cb: ReleaseHistoryCallback,
		scaleReqModifiers: RequestModifier<StreamScalesRequest>[] | null, // when null, scale requests are omitted
		deploymentReqModifiers: RequestModifier<StreamDeploymentsRequest>[] | null // when null, deployments are omitted
	) => CancelFunc;

	// write API
	updateApp: (app: App, cb: AppCallback) => CancelFunc;
	createScale: (req: CreateScaleRequest, cb: CreateScaleCallback) => CancelFunc;
	createRelease: (parentName: string, release: Release, cb: ReleaseCallback) => CancelFunc;
	createDeployment: (parentName: string, scale: CreateScaleRequest | null, cb: ErrorCallback) => CancelFunc;
}

interface AuthStatus {
	authenticated: boolean;
}

export type ErrorWithCode = Error & ServiceError;
export type CancelFunc = () => void;
export type AuthCallback = (s: AuthStatus | null, error: Error | null) => void;
export type AppsCallback = (res: StreamAppsResponse, error: ErrorWithCode | null) => void;
export type AppCallback = (app: App, error: ErrorWithCode | null) => void;
export type ReleasesCallback = (res: StreamReleasesResponse, error: ErrorWithCode | null) => void;
export type CreateScaleCallback = (sr: ScaleRequest, error: ErrorWithCode | null) => void;
export type ReleaseCallback = (release: Release, error: ErrorWithCode | null) => void;
export type ErrorCallback = (error: ErrorWithCode | null) => void;
export type ScaleRequestsCallback = (res: StreamScalesResponse, error: ErrorWithCode | null) => void;
export type DeploymentsCallback = (res: StreamDeploymentsResponse, error: ErrorWithCode | null) => void;
export type ReleaseHistoryCallback = (res: StreamReleaseHistoryResponse, error: ErrorWithCode | null) => void;

export class ReleaseHistoryItem {
	private _scale: ScaleRequest | null;
	private _deployment: ExpandedDeployment | null;

	public isScaleRequest: boolean;
	public isDeployment: boolean;

	constructor(scale: ScaleRequest | null, deployment: ExpandedDeployment | null) {
		this._scale = scale;
		this.isScaleRequest = !!scale;
		this._deployment = deployment;
		this.isDeployment = !!deployment;
	}

	public getName(): string {
		if (this._scale) {
			return this._scale.getName();
		}
		if (this._deployment) {
			return this._deployment.getName();
		}
		return '';
	}

	public getScaleRequest(): ScaleRequest {
		return this._scale || new ScaleRequest();
	}

	public getDeployment(): ExpandedDeployment {
		return this._deployment || new ExpandedDeployment();
	}
}

export class StreamReleaseHistoryResponse {
	private _scalesRes: StreamScalesResponse | null;
	private _deploymentsRes: StreamDeploymentsResponse | null;
	private _items: ReleaseHistoryItem[];
	private _itemsBuilt: boolean;

	constructor(scalesRes: StreamScalesResponse | null, deploymentsRes: StreamDeploymentsResponse | null) {
		this._scalesRes = scalesRes;
		this._deploymentsRes = deploymentsRes;
		this._items = [];
		this._itemsBuilt = false;
	}

	public isComplete(): boolean {
		return !!(this._scalesRes && this._deploymentsRes);
	}

	public getItemsList(): ReleaseHistoryItem[] {
		if (!this._itemsBuilt) {
			this._buildItems();
		}
		return this._items;
	}

	public getDeploymentsList(): ExpandedDeployment[] {
		if (this._deploymentsRes) {
			return this._deploymentsRes.getDeploymentsList();
		}
		return [];
	}

	public getScaleRequestsList(): ScaleRequest[] {
		if (this._scalesRes) {
			return this._scalesRes.getScaleRequestsList();
		}
		return [];
	}

	public getDeploymentsNextPageToken(): string {
		if (this._deploymentsRes) {
			return this._deploymentsRes.getNextPageToken();
		}
		return '';
	}

	public getScaleRequestsNextPageToken(): string {
		if (this._scalesRes) {
			return this._scalesRes.getNextPageToken();
		}
		return '';
	}

	public receiveStreamScalesResponse(res: StreamScalesResponse): StreamReleaseHistoryResponse {
		if (this._scalesRes) {
			res = mergeStreamScalesResponses(this._scalesRes, res);
		}
		return new StreamReleaseHistoryResponse(res, this._deploymentsRes);
	}

	public receiveStreamDeploymentsResponse(res: StreamDeploymentsResponse): StreamReleaseHistoryResponse {
		if (this._deploymentsRes) {
			res = mergeStreamDeploymentResponses(this._deploymentsRes, res);
		}
		return new StreamReleaseHistoryResponse(this._scalesRes, res);
	}

	private _buildItems(): void {
		const items = [] as ReleaseHistoryItem[];
		const deployments = this.getDeploymentsList();
		const scales = this.getScaleRequestsList();
		const dlen = deployments.length;
		const slen = scales.length;
		let di = 0;
		let si = 0;
		while (di < dlen || si < slen) {
			const d = deployments[di];
			const dt = d ? (d.getCreateTime() as timestamp_pb.Timestamp).toDate() : null;
			const s = scales[si];
			const st = s ? (s.getCreateTime() as timestamp_pb.Timestamp).toDate() : null;
			let item: ReleaseHistoryItem;

			if ((dt && st && dt > st) || (dt && !st)) {
				item = new ReleaseHistoryItem(null, d);
				di++;
			} else if (st) {
				item = new ReleaseHistoryItem(s, null);
				si++;
			} else {
				break;
			}

			items.push(item);
		}

		this._items = items;
		this._itemsBuilt = true;
	}
}

export type RequestModifier<T> = {
	(req: T): void;
	key: string;
};

export interface PaginatableRequest {
	getPageSize(): number;
	setPageSize(value: number): void;

	getPageToken(): string;
	setPageToken(value: string): void;
}

export function setPageSize(pageSize: number): RequestModifier<PaginatableRequest> {
	return Object.assign(
		(req: PaginatableRequest) => {
			req.setPageSize(pageSize);
		},
		{ key: `pageSize--${pageSize}` }
	);
}

export function setPageToken(pageToken: string): RequestModifier<PaginatableRequest> {
	return Object.assign(
		(req: PaginatableRequest) => {
			req.setPageToken(pageToken);
		},
		{ key: `pageToken--${pageToken}` }
	);
}

export interface NameFilterable {
	clearNameFiltersList(): void;
	getNameFiltersList(): Array<string>;
	setNameFiltersList(value: Array<string>): void;
	addNameFilters(value: string, index?: number): string;
}

export function setNameFilters(...filterNames: string[]): RequestModifier<NameFilterable> {
	return Object.assign(
		(req: NameFilterable) => {
			req.setNameFiltersList(filterNames);
		},
		{ key: `nameFilters--${filterNames.join('|')}` }
	);
}

export interface CreateStreamable {
	setStreamCreates(value: boolean): void;
}

export function setStreamCreates(): RequestModifier<CreateStreamable> {
	return Object.assign(
		(req: CreateStreamable) => {
			req.setStreamCreates(true);
		},
		{ key: 'streamCreates' }
	);
}

export interface UpdateStreamable {
	setStreamUpdates(value: boolean): void;
}

export function setStreamUpdates(): RequestModifier<UpdateStreamable> {
	return Object.assign(
		(req: UpdateStreamable) => {
			req.setStreamUpdates(true);
		},
		{ key: 'streamUpdates' }
	);
}

export function listDeploymentsRequestFilterType(
	...filterTypes: Array<ReleaseTypeMap[keyof ReleaseTypeMap]>
): RequestModifier<StreamDeploymentsRequest> {
	return Object.assign(
		(req: StreamDeploymentsRequest) => {
			req.setTypeFiltersList(filterTypes);
		},
		{ key: `filterTypes--${filterTypes.join('|')}` }
	);
}

export function setDeploymentStatusFilters(
	...statusFilters: Array<DeploymentStatusMap[keyof DeploymentStatusMap]>
): RequestModifier<StreamDeploymentsRequest> {
	return Object.assign(
		(req: StreamDeploymentsRequest) => {
			req.setStatusFiltersList(statusFilters);
		},
		{ key: `filterStatus--${statusFilters.join('|')}` }
	);
}

export function excludeAppsWithLabels(labels: [string, string][]): RequestModifier<StreamAppsRequest> {
	return Object.assign(
		(req: StreamAppsRequest) => {
			labels.forEach(([key, val]: [string, string]) => {
				const f = new LabelFilter();
				const e = new LabelFilter.Expression();
				e.setKey(key);
				e.addValues(val);
				e.setOp(LabelFilter.Expression.Operator.OP_NOT_IN);
				f.addExpressions(e);
				req.addLabelFilters(f);
			});
		},
		{ key: `excludeAppsWithLabels--${JSON.stringify(labels)}` }
	);
}

export function filterScalesByState(
	...stateFilters: Array<ScaleRequestStateMap[keyof ScaleRequestStateMap]>
): RequestModifier<StreamScalesRequest> {
	return Object.assign(
		(req: StreamScalesRequest) => {
			req.setStateFiltersList(stateFilters);
		},
		{ key: `stateFilters--${JSON.stringify(stateFilters)}` }
	);
}

const UnknownError: ErrorWithCode = Object.assign(new Error('Unknown error'), {
	code: grpc.Code.Unknown,
	metadata: new grpc.Metadata()
});

export function isNotFoundError(error: Error): boolean {
	return (error as ErrorWithCode).code === grpc.Code.NotFound;
}

interface Cancellable {
	cancel(): void;
	on(typ: 'end', handler: () => void): void;
}

enum BuildCancelFuncOpts {
	CONFIRM_CANCEL
}

function buildCancelFunc<T>(req: Cancellable, ..._opts: BuildCancelFuncOpts[]): CancelFunc {
	const opts = new Set(_opts);
	let cancelled = false;
	req.on('end', () => {
		cancelled = true;
	});
	function cancel() {
		if (cancelled) return;
		if (opts.has(BuildCancelFuncOpts.CONFIRM_CANCEL)) {
			if (
				!window.confirm(
					'This page is asking you to confirm that you want to cancel a network request - data you have entered may not be saved.'
				)
			) {
				return;
			}
		}
		cancelled = true;
		window.removeEventListener('beforeunload', handleBeforeUnload);
		req.cancel();
	}
	function handleBeforeUnload() {
		cancel();
	}
	window.addEventListener('beforeunload', handleBeforeUnload);
	return cancel;
}

function convertServiceError(error: ServiceError): ErrorWithCode {
	return Object.assign(new Error(error.message), error);
}

function buildStatusError(s: Status): ErrorWithCode {
	return Object.assign(new Error(s.details), s);
}

function buildStreamErrorHandler<T>(stream: ResponseStream<T>, cb: (error: ErrorWithCode) => void) {
	stream.on('status', (s: Status) => {
		if (s.code !== grpc.Code.OK) {
			cb(buildStatusError(s));
		}
	});
}

function compareTimestamps(a: Timestamp | undefined, b: Timestamp | undefined): 1 | 0 | -1 {
	const ad = (a || new Timestamp()).toDate();
	const bd = (b || new Timestamp()).toDate();
	if (ad === bd) {
		return 0;
	}
	if (ad > bd) {
		return 1;
	}
	return -1;
}

const __memoizedStreams = {} as { [key: string]: ResponseStream<any> };
const __memoizedStreamN = {} as { [key: string]: number };
const __memoizedStreamResponses = {} as { [key: string]: any };
function memoizedStream<T>(
	contextKey: string,
	streamKey: string,
	opts: { init: () => ResponseStream<T>; mergeResponses: (prev: T | null, res: T) => T }
): [ResponseStream<T>, T | undefined] {
	const key = contextKey + streamKey;
	function cleanup(streamEnded = false) {
		const n = (__memoizedStreamN[key] = (__memoizedStreamN[key] || 0) - 1);
		if (n === 0 || streamEnded) {
			delete __memoizedStreams[key];
			delete __memoizedStreamN[key];
			delete __memoizedStreamResponses[key];
		}
		return n;
	}

	__memoizedStreamN[key] = (__memoizedStreamN[key] || 0) + 1;

	let stream = __memoizedStreams[key];
	if (stream) {
		return [stream as ResponseStream<T>, __memoizedStreamResponses[key] as T | undefined];
	}
	let dataCallbacks = [] as Array<(data: T) => void>;
	stream = opts.init();
	stream.on('data', (data: T) => {
		data = opts.mergeResponses(__memoizedStreamResponses[key] || null, data);
		__memoizedStreamResponses[key] = data;
		dataCallbacks.forEach((cb) => cb(data));
	});
	let cancel = stream.cancel;
	stream.on('end', (status?: Status) => {
		cleanup(true);
		cancel = () => {};
	});
	stream.cancel = () => {
		if (cleanup() === 0) {
			cancel();
		}
	};
	const s = {
		on: (typ: string, handler: Function): ResponseStream<T> => {
			switch (typ) {
				case 'data':
					dataCallbacks.push(handler as (message: T) => void);
					break;
				case 'end':
					stream.on('end', handler as (status?: Status) => void);
					break;
				case 'status':
					stream.on('status', handler as (status: Status) => void);
					break;
				default:
			}
			return s;
		},
		cancel: stream.cancel
	};
	__memoizedStreams[key] = s;
	return [s, undefined];
}

function retryStream<T>(init: () => ResponseStream<T>): ResponseStream<T> {
	let nRetries = 0;
	const maxRetires = 3;
	let retryTimeoutMs = 1000;
	let retryTimeoutId: ReturnType<typeof setTimeout>;

	let stream = init();
	let hasResponse = false;
	let handlers = new Map<'data' | 'status' | 'end', Function[]>();
	const on = (typ: 'data' | 'status' | 'end', handler: Function): ResponseStream<T> => {
		handlers.set(typ, (handlers.get(typ) || []).concat([handler]));
		if (typ === 'data') {
			stream.on(typ as any, handler as any);
		} else {
			stream.on(typ as any, (...args) => {
				// only call upstream 'status' and 'end' handlers when there is
				// either a response or no more retries will occur
				if (hasResponse || nRetries === maxRetires) {
					handler.apply(undefined, args);
				}
			});
		}
		return stream;
	};
	const retryOnEnd = (status?: Status) => {
		if (status && status.code === grpc.Code.Unknown) {
			// reconnect retry handler unless maxRetries reached
			if (nRetries++ <= maxRetires) {
				retryTimeoutId = setTimeout(() => {
					// retry
					stream = init();

					// reconnect event handlers
					handlers.forEach((fns, typ) => {
						fns.forEach((handler) => {
							on(typ, handler);
						});
					});

					stream.on('end', retryOnEnd);
				}, retryTimeoutMs);
				retryTimeoutMs += 10000;
			}
		}
	};
	stream.on('data', () => {
		hasResponse = true;
	});
	stream.on('end', retryOnEnd);

	return {
		on,
		cancel: () => {
			clearTimeout(retryTimeoutId);
			stream.cancel();
		}
	};
}

function mergeStreamScalesResponses(
	prev: StreamScalesResponse | null,
	res: StreamScalesResponse
): StreamScalesResponse {
	const scaleIndices = new Map<string, number>();
	const scales = [] as ScaleRequest[];
	(prev ? prev.getScaleRequestsList() : []).forEach((scale, index) => {
		scaleIndices.set(scale.getName(), index);
		scales.push(scale);
	});
	res.getScaleRequestsList().forEach((scale) => {
		const index = scaleIndices.get(scale.getName());
		if (index !== undefined) {
			scales[index] = scale;
		} else {
			scales.push(scale);
		}
	});
	scales.sort((a, b) => {
		return compareTimestamps(b.getCreateTime(), a.getCreateTime());
	});
	res.setScaleRequestsList(scales);
	return res;
}

function mergeStreamDeploymentResponses(
	prev: StreamDeploymentsResponse | null,
	res: StreamDeploymentsResponse
): StreamDeploymentsResponse {
	const deploymentIndices = new Map<string, number>();
	const deployments = [] as ExpandedDeployment[];
	(prev ? prev.getDeploymentsList() : []).forEach((deployment, index) => {
		deploymentIndices.set(deployment.getName(), index);
		deployments.push(deployment);
	});
	res.getDeploymentsList().forEach((deployment) => {
		const index = deploymentIndices.get(deployment.getName());
		if (index !== undefined) {
			deployments[index] = deployment;
		} else {
			deployments.push(deployment);
		}
	});
	res.setDeploymentsList(
		deployments.sort((a, b) => {
			return compareTimestamps(b.getCreateTime(), a.getCreateTime());
		})
	);
	return res;
}

class _Client implements Client {
	private _cc: ControllerClient;
	constructor(cc: ControllerClient) {
		this._cc = cc;
	}

	public login(token: string, cb: AuthCallback): CancelFunc {
		var controller = new AbortController();
		var signal = controller.signal;
		fetch('/login', {
			method: 'POST',
			signal,
			mode: 'cors',
			cache: 'no-cache',
			credentials: 'same-origin',
			headers: {
				'Content-Type': 'application/json'
			},
			body: JSON.stringify({ token })
		})
			.then((response) => {
				if (response.ok) {
					return response.json();
				} else {
					return response.json().then((errorJSON) => {
						throw new Error(`[${errorJSON.code}] ${errorJSON.message}`);
					});
				}
			})
			.then((conf: PrivateConfig) => {
				Object.assign(Config, conf);
				cb({ authenticated: true }, null);
			})
			.catch((error) => {
				cb(null, error);
			});
		return () => {
			controller.abort();
		};
	}

	public logout(cb: AuthCallback): CancelFunc {
		var controller = new AbortController();
		var signal = controller.signal;
		fetch('/logout', {
			method: 'POST',
			signal,
			mode: 'cors',
			cache: 'no-cache',
			credentials: 'same-origin'
		})
			.then((response) => {
				if (response.ok) {
					Config.unsetPrivateConfig();
					cb({ authenticated: false }, null);
				} else {
					throw new Error(`Something went wrong logging out`);
				}
			})
			.catch((error) => {
				cb(null, error);
			});
		return () => {
			controller.abort();
		};
	}

	public streamApps(cb: AppsCallback, ...reqModifiers: RequestModifier<StreamAppsRequest>[]): CancelFunc {
		const streamKey = reqModifiers.map((m) => m.key).join(':');
		const [stream, lastResponse] = memoizedStream('streamApps', streamKey, {
			init: () => {
				return retryStream(() => {
					const req = new StreamAppsRequest();
					reqModifiers.forEach((m) => m(req));
					return this._cc.streamApps(req, this.metadata());
				});
			},
			mergeResponses: (prev: StreamAppsResponse | null, res: StreamAppsResponse): StreamAppsResponse => {
				const appIndices = new Map<string, number>();
				const apps = [] as App[];
				(prev ? prev.getAppsList() : []).forEach((app, index) => {
					appIndices.set(app.getName(), index);
					apps.push(app);
				});
				res.getAppsList().forEach((app) => {
					const index = appIndices.get(app.getName());
					if (index !== undefined) {
						if (app.getDeleteTime() !== undefined) {
							app.setDisplayName(`${apps[index].getDisplayName()} [DELETED]`);
						}
						apps[index] = app;
					} else {
						apps.push(app);
					}
				});
				apps.sort((a, b) => {
					return a.getDisplayName().localeCompare(b.getDisplayName());
				});
				res.setAppsList(apps);
				return res;
			}
		});
		stream.on('data', (response: StreamAppsResponse) => {
			cb(response, null);
		});
		if (lastResponse) {
			cb(lastResponse, null);
		}
		buildStreamErrorHandler(stream, (error: ErrorWithCode) => {
			cb(new StreamAppsResponse(), error);
		});
		return buildCancelFunc(stream);
	}

	public streamReleases(cb: ReleasesCallback, ...reqModifiers: RequestModifier<StreamReleasesRequest>[]): CancelFunc {
		const streamKey = reqModifiers.map((m) => m.key).join(':');
		const [stream, lastResponse] = memoizedStream('streamReleases', streamKey, {
			init: () => {
				return retryStream(() => {
					const req = new StreamReleasesRequest();
					reqModifiers.forEach((m) => m(req));
					return this._cc.streamReleases(req, this.metadata());
				});
			},
			mergeResponses: (prev: StreamReleasesResponse | null, res: StreamReleasesResponse): StreamReleasesResponse => {
				const releaseIndices = new Map<string, number>();
				const releases = [] as Release[];
				(prev ? prev.getReleasesList() : []).forEach((release, index) => {
					releaseIndices.set(release.getName(), index);
					releases.push(release);
				});
				res.getReleasesList().forEach((release) => {
					const index = releaseIndices.get(release.getName());
					if (index !== undefined) {
						releases[index] = release;
					} else {
						releases.push(release);
					}
				});
				releases.sort((a, b) => {
					return compareTimestamps(b.getCreateTime(), a.getCreateTime());
				});
				res.setReleasesList(releases);
				return res;
			}
		});
		stream.on('data', (response: StreamReleasesResponse) => {
			cb(response, null);
		});
		if (lastResponse) {
			cb(lastResponse, null);
		}
		buildStreamErrorHandler(stream, (error: ErrorWithCode) => {
			cb(new StreamReleasesResponse(), error);
		});
		return buildCancelFunc(stream);
	}

	public streamScales(cb: ScaleRequestsCallback, ...reqModifiers: RequestModifier<StreamScalesRequest>[]): CancelFunc {
		const streamKey = reqModifiers.map((m) => m.key).join(':');
		const [stream, lastResponse] = memoizedStream('streamScales', streamKey, {
			init: () => {
				return retryStream(() => {
					const req = new StreamScalesRequest();
					reqModifiers.forEach((m) => m(req));
					return this._cc.streamScales(req, this.metadata());
				});
			},
			mergeResponses: mergeStreamScalesResponses
		});
		stream.on('data', (response: StreamScalesResponse) => {
			cb(response, null);
		});
		if (lastResponse) {
			cb(lastResponse, null);
		}
		buildStreamErrorHandler(stream, (error: ErrorWithCode) => {
			cb(new StreamScalesResponse(), error);
		});
		return buildCancelFunc(stream);
	}

	public streamDeployments(
		cb: DeploymentsCallback,
		...reqModifiers: RequestModifier<StreamDeploymentsRequest>[]
	): CancelFunc {
		const streamKey = reqModifiers.map((m) => m.key).join(':');
		const [stream, lastResponse] = memoizedStream('streamDeployments', streamKey, {
			init: () => {
				return retryStream(() => {
					const req = new StreamDeploymentsRequest();
					reqModifiers.forEach((m) => m(req));
					return this._cc.streamDeployments(req, this.metadata());
				});
			},
			mergeResponses: mergeStreamDeploymentResponses
		});
		let hasData = false;
		stream.on('data', (response: StreamDeploymentsResponse) => {
			hasData = true;
			cb(response, null);
		});
		stream.on('end', (status?: Status) => {
			if (hasData) return;
			// make sure cb is called
			cb(new StreamDeploymentsResponse(), null);
		});
		if (lastResponse) {
			cb(lastResponse, null);
		}
		buildStreamErrorHandler(stream, (error: ErrorWithCode) => {
			cb(new StreamDeploymentsResponse(), error);
		});
		return buildCancelFunc(stream);
	}

	public streamReleaseHistory(
		cb: ReleaseHistoryCallback,
		scaleReqModifiers: RequestModifier<StreamScalesRequest>[] | null,
		deploymentReqModifiers: RequestModifier<StreamDeploymentsRequest>[] | null
	): CancelFunc {
		let streamScalesRes: StreamScalesResponse;
		let streamDeploymentsRes: StreamDeploymentsResponse;
		let streamReleaseHistoryRes: StreamReleaseHistoryResponse | null = null;

		const cancelStreamScales = scaleReqModifiers
			? ((reqModifiers: RequestModifier<StreamScalesRequest>[]) => {
					const stream = retryStream(() => {
						const req = new StreamScalesRequest();
						reqModifiers.forEach((m) => m(req));
						return this._cc.streamScales(req, this.metadata());
					});
					stream.on('data', (res: StreamScalesResponse) => {
						streamScalesRes = res;
						if (streamReleaseHistoryRes) {
							streamReleaseHistoryRes = streamReleaseHistoryRes.receiveStreamScalesResponse(res);
						} else {
							streamReleaseHistoryRes = new StreamReleaseHistoryResponse(res, streamDeploymentsRes);
						}
						cb(streamReleaseHistoryRes, null);
					});
					return stream.cancel;
			  })(scaleReqModifiers)
			: () => {};

		const cancelStreamDeployments = deploymentReqModifiers
			? ((reqModifiers: RequestModifier<StreamDeploymentsRequest>[]) => {
					const stream = retryStream(() => {
						const req = new StreamDeploymentsRequest();
						reqModifiers.forEach((m) => m(req));
						return this._cc.streamDeployments(req, this.metadata());
					});
					stream.on('data', (res: StreamDeploymentsResponse) => {
						streamDeploymentsRes = res;
						if (streamReleaseHistoryRes) {
							streamReleaseHistoryRes = streamReleaseHistoryRes.receiveStreamDeploymentsResponse(res);
						} else {
							streamReleaseHistoryRes = new StreamReleaseHistoryResponse(streamScalesRes, res);
						}
						cb(streamReleaseHistoryRes, null);
					});
					return stream.cancel;
			  })(deploymentReqModifiers)
			: () => {};

		return () => {
			cancelStreamScales();
			cancelStreamDeployments();
		};
	}

	public updateApp(app: App, cb: AppCallback): CancelFunc {
		// TODO(jvatic): implement update_mask to include only changed fields
		const req = new UpdateAppRequest();
		req.setApp(app);
		const onEndCallbacks = new Set<() => void>();
		return buildCancelFunc(
			Object.assign(
				this._cc.updateApp(req, this.metadata(), (error: ServiceError | null, response: App | null) => {
					onEndCallbacks.forEach((cb) => cb());

					if (response && error === null) {
						cb(response, null);
					} else if (error) {
						cb(new App(), convertServiceError(error));
					} else {
						cb(new App(), UnknownError);
					}
				}),
				{
					on: (typ: 'end', cb: () => void) => {
						onEndCallbacks.add(cb);
					}
				}
			),
			BuildCancelFuncOpts.CONFIRM_CANCEL
		);
	}

	public createScale(req: CreateScaleRequest, cb: CreateScaleCallback): CancelFunc {
		const onEndCallbacks = new Set<() => void>();
		return buildCancelFunc(
			Object.assign(
				this._cc.createScale(req, this.metadata(), (error: ServiceError | null, response: ScaleRequest | null) => {
					onEndCallbacks.forEach((cb) => cb());

					if (response && error === null) {
						cb(response, null);
					} else if (error) {
						cb(new ScaleRequest(), convertServiceError(error));
					} else {
						cb(new ScaleRequest(), UnknownError);
					}
				}),
				{
					on: (typ: 'end', cb: () => void) => {
						onEndCallbacks.add(cb);
					}
				}
			),
			BuildCancelFuncOpts.CONFIRM_CANCEL
		);
	}

	public createRelease(parentName: string, release: Release, cb: ReleaseCallback): CancelFunc {
		const req = new CreateReleaseRequest();
		req.setParent(parentName);
		req.setRelease(release);
		const onEndCallbacks = new Set<() => void>();
		return buildCancelFunc(
			Object.assign(
				this._cc.createRelease(req, this.metadata(), (error: ServiceError | null, response: Release | null) => {
					onEndCallbacks.forEach((cb) => cb());

					if (response && error === null) {
						cb(response, null);
					} else if (error) {
						cb(new Release(), convertServiceError(error));
					} else {
						cb(new Release(), UnknownError);
					}
				}),
				{
					on: (typ: 'end', cb: () => void) => {
						onEndCallbacks.add(cb);
					}
				}
			),
			BuildCancelFuncOpts.CONFIRM_CANCEL
		);
	}

	public createDeployment(parentName: string, scale: CreateScaleRequest | null, cb: ErrorCallback): CancelFunc {
		const req = new CreateDeploymentRequest();
		req.setParent(parentName);
		if (scale) {
			req.setScaleRequest(scale);
		}

		const stream = this._cc.createDeployment(req, this.metadata());
		stream.on('status', (s: Status) => {
			if (s.code === grpc.Code.OK) {
				cb(null);
			} else {
				cb(buildStatusError(s));
			}
		});
		stream.on('end', () => {});
		return buildCancelFunc(stream, BuildCancelFuncOpts.CONFIRM_CANCEL);
	}

	private metadata(): grpc.Metadata {
		const headers = new BrowserHeaders({});
		if (Config.CONTROLLER_AUTH_KEY) {
			headers.set('Authorization', `Basic ${btoa(['', Config.CONTROLLER_AUTH_KEY].join(':'))}`);
			headers.set('Auth-Key', Config.CONTROLLER_AUTH_KEY);
		}
		return headers;
	}
}

const cc = new ControllerClient(Config.CONTROLLER_HOST, { debug: false });

export default new _Client(cc);
