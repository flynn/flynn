import * as React from 'react';
import { Button as GrommetButton, ButtonProps } from 'grommet';
import { Omit } from 'grommet/utils';
import styled from 'styled-components';

const StyledButton = styled(GrommetButton)`
	border-radius: 1.82em;
`;

interface Props extends ButtonProps, Omit<JSX.IntrinsicElements['button'], 'color'> {}

const Button: React.FC<Props> = function Button(props: Props, ref: any) {
	return <StyledButton {...props} ref={ref} />;
};
export default React.forwardRef(Button);
