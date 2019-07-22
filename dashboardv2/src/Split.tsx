import * as React from 'react';
import { Box } from 'grommet';

interface Props {
	flex?: 'left' | 'right' | 'both';
	separator?: boolean;
	children: React.ReactNodeArray;
}

export default ({ children, flex, separator, ...rest }: Props) => {
	const adjustedChildren = React.Children.map(children, (child, index) => {
		if (child && ((index > 0 && flex === 'right') || (index === 0 && flex === 'left'))) {
			return (
				<Box
					flex={true}
					fill="vertical"
					border={separator ? { side: index ? 'left' : 'right', color: 'black' } : undefined}
				>
					{child}
				</Box>
			);
		}
		return child;
	});
	return (
		<Box direction="row" fill={true} {...rest}>
			{adjustedChildren}
		</Box>
	);
};
