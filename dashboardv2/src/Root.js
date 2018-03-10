import React, { PureComponent } from 'react'
import {
  BrowserRouter as Router,
  Switch,
  Route
} from 'react-router-dom'
import { Split, Box } from 'grommet'
import AppListing from './components/AppListing'
import PageLanding from './pages/PageLanding'

export default class Root extends PureComponent {
  render () {
    return (
      <Split flex="right">
        <Box colorIndex='neutral-1'
          full='vertical'
          size='small'>
          <AppListing />
        </Box>
        <Box
          full='vertical'>
          <Router>
            <Switch>
              <Route exact path="/" component={PageLanding}/>
            </Switch>
          </Router>
        </Box>
      </Split>
    )
  }
}
