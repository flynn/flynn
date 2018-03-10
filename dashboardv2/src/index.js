import React from 'react'
import ReactDOM from 'react-dom'
import 'grommet-css'
import './index.css'
import Root from './Root'
import registerServiceWorker from './registerServiceWorker'

ReactDOM.render(<Root />, document.getElementById('root'))
registerServiceWorker()
