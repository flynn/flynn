import * as jspb from 'google-protobuf';
import { Release, ProcessType } from '../generated/controller_pb';

// ensures all process types from release are included in scale request
// processes map
export default function buildProcessesMap(
	scaleRequestProcesses: jspb.Map<string, number>,
	release: Release | null
): jspb.Map<string, number> {
	if (!release) return scaleRequestProcesses;
	return release
		.getProcessesMap()
		.toArray()
		.reduce((m: jspb.Map<string, number>, [key, pt]: [string, ProcessType]) => {
			if (!m.has(key)) {
				m.set(key, 0);
			}
			return m;
		}, scaleRequestProcesses);
}
