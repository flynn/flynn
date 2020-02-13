import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box } from 'grommet';

import { Release } from './generated/controller_pb';
import KeyValueDiff from './KeyValueDiff';
import useRelativeTimeString from './useRelativeTimeString';

export interface ReleaseProps {
	release: Release;
	prevRelease?: Release | null;
}

function ReleaseComponent({ release, prevRelease: prev }: ReleaseProps) {
	const createTime = ((createTime) => (createTime ? createTime.toDate() : undefined))(release.getCreateTime());
	const relativeTimeString = useRelativeTimeString(createTime || new Date());

	return (
		<Box flex="grow">
			{relativeTimeString ? (
				<>
					{relativeTimeString}
					<br />
				</>
			) : null}
			<KeyValueDiff prev={prev ? prev.getEnvMap() : new jspb.Map([])} next={release.getEnvMap()} />
		</Box>
	);
}

export default React.memo(ReleaseComponent);
