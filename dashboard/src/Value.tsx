import * as React from 'react';
import { Box, Text } from 'grommet';

const VALUE_SIZES = {
	xsmall: 'medium',
	small: 'large',
	medium: 'xlarge',
	large: 'xlarge',
	xlarge: 'xlarge'
};

interface Props {
	active?: boolean;
	align?: 'start' | 'center' | 'end';
	announce?: boolean;
	colorIndex?: string;
	icon?: React.ReactNode;
	label?: string | React.ReactNode;
	onClick?: Function;
	size?: 'xsmall' | 'small' | 'medium' | 'large' | 'xlarge';
	trendIcon?: React.ReactNode;
	value?: React.ReactNode | number | string;
	units?: React.ReactNode | string;
}

export default ({ colorIndex, icon, label, size = 'small', trendIcon, units, value }: Props) => (
	<Box align="center">
		<Box direction="row" align="center">
			{icon}
			<Text color={colorIndex} size={VALUE_SIZES[size]}>
				<strong>{value}</strong>
			</Text>
			{units ? (
				<Box margin={{ left: 'xsmall' }}>
					<Text color={colorIndex} size={VALUE_SIZES[size]}>
						{units}
					</Text>
				</Box>
			) : null}
			{trendIcon}
		</Box>
		{label ? <Text>{label}</Text> : null}
	</Box>
);
