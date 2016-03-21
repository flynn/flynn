/* globals $:false, EventSource:false */
$(function() {
  "use strict";
  var logs   = $("#logs");
  var stream = new EventSource(document.location.href);

  stream.onmessage = function(e) {
    var line = JSON.parse(e.data);

    // convert ANSI colour codes to HTML spans
    line = ansi_up.ansi_to_html(line);

    // replace single CR with CRLF
    line = line.replace(/\r([^\n])/g, "\r\n$1");

    // append the line to the log
    logs.append(line + "\n");
  };

  stream.onerror = function() {
    stream.close();
  };
});
