import * as React from 'react';
import { Layer, Box } from 'grommet';
import styled from 'styled-components';
import theme from './theme';

// NOTE: Grommet doesn't include the css vars automatically, so we have to
// include them here so they're available to any component in this tree.
const StyledLayer = styled(Layer)`
	min-width: 50vw;
	max-width: 75vw;
	${(props) => {
		return Object.keys(props.theme.global.colors)
			.filter((k) => typeof props.theme.global.colors[k] === 'string')
			.map((k) => `--${k}: ${props.theme.global.colors[k]};`)
			.join('\n');
	}}
`;

export interface Props {
	children: React.ReactNode;
	onClose?: () => void;
}

export default function RightOverlay({ children, onClose }: Props) {
	return (
		<StyledLayer position="right" onClickOutside={onClose} onEsc={onClose} full="vertical" theme={theme}>
			<Box fill pad="small" overflow="scroll">
				{children}
			</Box>
		</StyledLayer>
	);
}
