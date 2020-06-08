// import ifDev from './ifDev';

export default function debug(msg: string, ...args: any[]) {
	// TODO(jvatic): reinstate ifDev check once we're sure things are working
	// ifDev(() => {
	console.log(`[DEBUG]: ${msg}`, ...args);
	// });
}
