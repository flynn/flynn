import * as React from 'react';
import { Box, BoxProps, Text, Button } from 'grommet';
import {
	StatusCritical,
	StatusDisabled,
	StatusGood,
	StatusUnknown,
	StatusWarning,
	Close as CloseIcon
} from 'grommet-icons';

interface NotificationProps extends BoxProps {
	onClose?: () => void;
	message: string;
	status?: 'critical' | 'disabled' | 'ok' | 'unknown' | 'warning';
}

const VALUE_ICON = {
	critical: StatusCritical,
	disabled: StatusDisabled,
	ok: StatusGood,
	unknown: StatusUnknown,
	warning: StatusWarning
} as { [key: string]: any };

const StatusIcon = ({ value, ...rest }: { value: string; color: string }) => {
	const Icon = VALUE_ICON[value.toLowerCase()] || StatusUnknown;
	return <Icon color={`status-${value.toLowerCase()}`} {...rest} />;
};

export default ({ message, status, onClose, ...rest }: NotificationProps) => (
	<Box
		direction="row"
		pad="small"
		align="center"
		justify="between"
		background={status ? `status-${status.toLowerCase()}` : undefined}
		{...rest}
	>
		{status ? (
			<Box margin={{ right: 'medium' }}>
				<StatusIcon value={status} color="white" />
			</Box>
		) : null}
		{message ? <Text>{message}</Text> : null}
		{onClose ? (
			<Box margin={{ right: 'medium' }} onClick={onClose}>
				<Button>
					<CloseIcon color="white" />
				</Button>
			</Box>
		) : (
			<div>&nbsp;</div>
		)}
	</Box>
);
