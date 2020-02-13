import * as React from 'react';
import { Box, Grid, GridProps } from 'grommet';

import { Release, ProcessType, Port } from './generated/controller_pb';

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

						<h5>Args</h5>
						{pt.getArgsList().join(' ')}

						<h5>Environment Variables</h5>
						<pre>{JSON.stringify(pt.getEnvMap().toObject(), undefined, 2)}</pre>

						<h5>Ports</h5>
						{pt.getPortsList().map((p: Port) => {
							return (
								<pre key={p.getPort()}>
									Port: {p.getPort()}
									Proto: {p.getProto()}
									Service:{' '}
									{((s) =>
										s
											? `
												Name: ${s.getDisplayName()}
												Create: ${s.getCreate().toString()}
												HealthCheck: ${s.hasCheck().toString()} (TODO: show details)
											`
											: 'N/A')(p.getService())}
								</pre>
							);
						})}

						<h5>Volumes</h5>
						{pt.getVolumesList().map((vr) => {
							return (
								<pre key={vr.getPath()}>
									Path: {vr.getPath()}
									Delete on stop: {vr.getDeleteOnStop().toString()}
								</pre>
							);
						})}
					</Box>
				);
				return m;
			}, [] as React.ReactNodeArray)}
		</Grid>
	);
}
