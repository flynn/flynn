var http = require('http');
var port = process.env.PORT || 5000;

http.createServer(function (req, res) {
  res.writeHead(200, {'Content-Type': 'text/plain'});
  res.end('Hello to Yahoo from Flynn on port '+port+'\n');
}).listen(port, function() {
  console.log("Listening on " + port);
});
