import React, { PureComponent } from 'react'
import { Split, Box } from 'grommet'

export default class Root extends PureComponent {
  render () {
    return (
      <Split flex="right">
        <Box colorIndex='neutral-1'
          full='vertical'
          size='small'>
          TODO: Main Navigation
        </Box>
        <Box
          full='vertical'>
          TODO: Component Routing
        </Box>
      </Split>
    )
  }
}
