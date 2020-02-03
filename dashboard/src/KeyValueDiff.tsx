import * as React from 'react';
import * as jspb from 'google-protobuf';
import styled from 'styled-components';
import { Box } from 'grommet';
import protoMapDiff, { DiffOption } from './util/protoMapDiff';

type StringMap = jspb.Map<string, string>;

const changeOps = new Set(['add', 'remove']);
const opBackgroundColors = {
	remove: 'rgba(255, 0, 0, 0.075)',
	add: 'rgba(0, 255, 0, 0.075)'
} as { [key: string]: string };
interface DiffLineProps {
	op: string;
}
const DiffLine = styled(Box)<DiffLineProps>`
	white-space: pre-wrap;
	word-break: break-all;
	line-height: 1.2em;
	max-width: 40vw;
	font-weight: ${(props) => (changeOps.has(props.op) ? 'bold' : 'normal')};
	background-color: ${(props) => opBackgroundColors[props.op] || 'transparent'};
`;

export interface KeyValueDiffProps {
	prev: StringMap;
	next: StringMap;
}

export default function KeyValueDiff({ prev, next }: KeyValueDiffProps) {
	const diff = protoMapDiff(prev, next, DiffOption.INCLUDE_UNCHANGED).sort((a, b) => {
		return a.key.localeCompare(b.key);
	});

	return (
		<Box tag="pre">
			{diff.map((item) => {
				let value;
				let prefix = ' ';
				switch (item.op) {
					case 'keep':
						value = next.get(item.key);
						break;
					case 'remove':
						prefix = '-';
						value = prev.get(item.key);
						break;
					case 'add':
						prefix = '+';
						value = next.get(item.key);
						break;
					default:
						break;
				}
				return (
					<DiffLine as="span" key={item.op + item.key} op={item.op}>
						{prefix} {item.key} = {value}
					</DiffLine>
				);
			})}
		</Box>
	);
}
