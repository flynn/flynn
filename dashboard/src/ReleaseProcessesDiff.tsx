import * as React from 'react';
import { Box, Grid, GridProps } from 'grommet';

import { Release, ProcessType } from './generated/controller_pb';

interface Props extends GridProps {
	release: Release;
	prevRelease?: Release;
}

export default function ProcessesDiff({ release, prevRelease = new Release(), ...gridProps }: Props) {
	const processes = [] as Array<[string, ProcessType]>;
	release.getProcessesMap().forEach((pt: ProcessType, key: string) => {
		processes.push([key, pt]);
	});
	return (
		<Grid justify="start" columns="small" gap="small" {...gridProps}>
			{processes.reduce((m: React.ReactNodeArray, [key, pt]: [string, ProcessType]) => {
				m.push(
					<Box key={key}>
						<h4>{key}</h4>
					</Box>
				);
				return m;
			}, [] as React.ReactNodeArray)}
		</Grid>
	);
}
