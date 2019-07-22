import * as React from 'react';

import { Anchor, AnchorProps } from 'grommet';
import { Omit } from 'grommet/utils';

export interface Props extends Omit<AnchorProps, 'onClick'> {
	onClick?: (event: React.MouseEvent) => void;
	children: React.ReactNode;
}

export default function ExternalAnchor(props: Props) {
	const { onClick, children, ...rest } = props;
	return (
		<Anchor
			{...rest}
			onClick={(e: React.MouseEvent) => {
				const defaultOnClick = onClick || (() => {});

				defaultOnClick(e);

				if (e.isPropagationStopped()) {
					return;
				}

				// don't open in new window if any modifier keys are pressed
				if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) {
					return;
				}

				e.preventDefault();
				window.open(props.href);
			}}
		>
			{children}
		</Anchor>
	);
}
