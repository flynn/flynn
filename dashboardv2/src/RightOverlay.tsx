import * as React from 'react';
import { Layer, Box } from 'grommet';

export interface Props {
	children: React.ReactNode;
	onClose: () => void;
}

export default function RightOverlay({ children, onClose }: Props) {
	return (
		<Layer position="right" onClickOutside={onClose} onEsc={onClose} full="vertical">
			<Box fill pad="small" overflow="scroll">
				{children}
			</Box>
		</Layer>
	);
}
