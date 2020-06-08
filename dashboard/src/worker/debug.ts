import _debug from '../debug';
export default function debug(msg: string, ...args: any[]) {
	_debug(`[SW]: ${msg}`, ...args);
}
