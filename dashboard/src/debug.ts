import ifDev from './ifDev';

export default function debug(msg: string, ...args: any[]) {
	ifDev(() => {
		console.log(`[DEBUG]: ${msg}`, ...args);
	});
}
