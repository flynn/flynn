import * as jspb from 'google-protobuf';

export default function protoMapToObject<K, V>(m: jspb.Map<K, V>): any {
	const obj = {} as any;
	m.forEach((v: V, k: K) => {
		obj[k] = v;
	});
	return obj;
}
