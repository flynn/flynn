import * as React from 'react';
import { Layer, Box } from 'grommet';
import styled from 'styled-components';

const StyledLayer = styled(Layer)`
	min-width: 50vw;
	max-width: 75vw;
`;

export interface Props {
	children: React.ReactNode;
	onClose: () => void;
}

export default function RightOverlay({ children, onClose }: Props) {
	return (
		<StyledLayer position="right" onClickOutside={onClose} onEsc={onClose} full="vertical">
			<Box fill pad="small" overflow="scroll">
				{children}
			</Box>
		</StyledLayer>
	);
}
