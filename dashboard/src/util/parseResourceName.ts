export interface ResourceNameMap {
	[resource: string]: string;
}

export default function parseResourceName(name: string): ResourceNameMap {
	const parts = name.split('/');
	const resourceNameMap = {} as ResourceNameMap;
	for (let i = 0; i < parts.length; i += 2) {
		if (i === parts.length) {
			break;
		}
		const resource = parts[i];
		const resourceID = parts[i + 1];
		resourceNameMap[resource] = resourceID;
	}
	return resourceNameMap;
}
