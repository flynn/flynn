import * as jspb from 'google-protobuf';

export default function protoMapReplace<K, V>(a: jspb.Map<K, V>, b: jspb.Map<K, V>) {
	a.clear();
	b.forEach((v: V, k: K) => {
		a.set(k, v);
	});
}
