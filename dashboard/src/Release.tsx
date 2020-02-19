import * as React from 'react';
import * as jspb from 'google-protobuf';
import { Box } from 'grommet';

import { Release } from './generated/controller_pb';
import KeyValueDiff from './KeyValueDiff';
import TimeAgo from './TimeAgo';

export interface ReleaseProps {
	timestamp?: Date;
	release: Release;
	prevRelease?: Release | null;
}

function ReleaseComponent({
	release,
	prevRelease: prev,
	timestamp = ((createTime) => (createTime ? createTime.toDate() : undefined))(release.getCreateTime())
}: ReleaseProps) {
	return (
		<Box flex="grow">
			{timestamp ? (
				<>
					<TimeAgo date={timestamp} />
					<br />
				</>
			) : null}
			<KeyValueDiff prev={prev ? prev.getEnvMap() : new jspb.Map([])} next={release.getEnvMap()} />
		</Box>
	);
}

export default React.memo(ReleaseComponent);
