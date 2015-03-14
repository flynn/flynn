package installer

import "text/template"

var htmlTemplate = template.Must(template.New("installer.html").Parse(`
<!doctype html>
<html>
<head>
  <title>Flynn Installer</title>
  <link rel="stylesheet" type="text/css" href="/assets/{{.ApplicationCSSPath}}" />
</head>

<body>
  <div id="main"></div>

  <script type="application/javascript" src="/assets/{{.ReactJSPath}}"></script>
  <script type="application/javascript" src="/assets/{{.ApplicationJSPath}}"></script>

  <noscript>
    <h1>You need JavaScript enabled to use this app!</h1>
  </noscript>
</body>
</html>
`))
