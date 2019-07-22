import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box } from 'grommet';

import { Release } from './generated/controller_pb';
import ExternalAnchor from './ExternalAnchor';
import KeyValueDiff from './KeyValueDiff';
import useApp from './useApp';

export interface ReleaseProps {
	release: Release;
	prevRelease?: Release | null;
}

function ReleaseComponent({ release, prevRelease: prev }: ReleaseProps) {
	// TODO(jvatic): Add parent field to Release and use that instead of `getName`
	const { app } = useApp(release.getName().split('/releases/')[0]);

	const labels = release.getLabelsMap();
	const appMeta = app ? app.getLabelsMap() : new jspb.Map([]);

	const gitCommit =
		labels.get('git.commit') ||
		(() => {
			const rev = labels.get('rev');
			if (labels.get('git') === 'true' && rev) {
				return rev;
			}
			return null;
		})();

	let githubURL = (appMeta.get('github.url') || null) as string | null;
	if (githubURL) {
		githubURL = `${githubURL.replace(/\/$/, '')}/commit/${gitCommit}`;
	} else if (labels.get('github') === 'true') {
		githubURL = `https://github.com/${labels.get('github_user')}/${labels.get('github_repo')}/commit/${gitCommit}`;
	}

	const releaseID = release
		.getName()
		.split('/')
		.slice(-1)[0];
	return (
		<Box flex="grow">
			{releaseID ? (
				<>
					Release {releaseID}
					<br />
				</>
			) : null}
			{gitCommit ? (
				<>
					<span>
						git.commit {githubURL ? <ExternalAnchor href={githubURL}>{gitCommit}</ExternalAnchor> : gitCommit}
					</span>
					<br />
				</>
			) : null}
			<KeyValueDiff prev={prev ? prev.getEnvMap() : new jspb.Map([])} next={release.getEnvMap()} />
		</Box>
	);
}

export default React.memo(ReleaseComponent);
