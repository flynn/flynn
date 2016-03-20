/* globals $:false, EventSource:false */
$(function() {
  "use strict";
  var logs   = $("#logs");
  var stream = new EventSource(document.location.href);

  stream.onmessage = function(e) {
    logs.append(JSON.parse(e.data) + "\n");
  };

  stream.onerror = function() {
    stream.close();
  };
});
