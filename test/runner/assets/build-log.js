/* globals $:false, _:false, EventSource:false */
$(function() {
  "use strict";
  var files         = {};
  var offsets       = [];
  var offsetIndex   = {};
  var listHovered   = false;
  var list          = $("#list");
  var logs          = $("#logs");
  var listTemplate  = _.template($("#list-template").html());
  var logsTemplate  = _.template($("#logs-template").html());
  var stream        = new EventSource(document.location.href);

  var adjustOffsetsTimeout = null;
  var adjustOffsets = function () {
    clearTimeout(adjustOffsetsTimeout);
    adjustOffsetsTimeout = setTimeout(function () {
      offsets = [];
      offsetIndex = {};
      for (var name in files) {
        if ( !files.hasOwnProperty(name) ) {
          continue;
        }
        files[name].calcOffset();
      }
      findActiveFile();
    }, 30);
  };

  var findActiveFile = function () {
    var scrollY = window.scrollY;
    var _offsets = offsets.sort(function (a, b) {
      return a - b;
    }).reverse();
    for (var i = 0, len = _offsets.length; i < len; i++) {
      if (scrollY >= (_offsets[i] - window.innerHeight / 2)) {
        offsetIndex[_offsets[i]].setActive();
        break;
      }
    }
  };

  var File = function (props) {
    files[props.filename] = this;
    this.filename = props.filename;
    this.$lines = props.$el.find('code');
    this.$el = props.$el;
    this.listItem = props.listItem;
    this.calcOffset();
  };

  File.prototype.calcOffset = function () {
    this.offsetTop = Math.round(this.$el.offset().top, 10);
    offsets.push(this.offsetTop);
    offsetIndex[this.offsetTop] = this;
  };

  File.prototype.appendLine = function (line) {
    this.$lines.append(line.text + "\n");
    adjustOffsets(this.offsetTop);
  };

  File.prototype.setActive = function () {
    for (var name in files) {
      if ( !files.hasOwnProperty(name) ) {
        continue;
      }
      files[name].unsetActive();
    }
    $(this.listItem).addClass('active');
    if ( !listHovered ) {
      this.listItem.scrollIntoView();
    }
  };

  File.prototype.unsetActive = function () {
    $(this.listItem).removeClass('active');
  };

  window.addEventListener('scroll', findActiveFile, false);
  list.hover(function () {
    listHovered = true;
  }, function () {
    listHovered = false;
  });

  stream.onmessage = function(e) {
    var listItem;
    var file;
    var line = JSON.parse(e.data);
    line.name = line.filename.slice(0, -4);

    if (files.hasOwnProperty(line.filename)) {
      file = files[line.filename];
    } else {
      listItem = document.createElement('p');
      listItem.innerHTML = listTemplate(line);
      list.append(listItem);
      logs.append(logsTemplate(line));
      file = new File({
        filename: line.filename,
        $el: $('#'+ line.name),
        listItem: listItem
      });
    }

    file.appendLine(line);
  };

  stream.onerror = function() {
    stream.close();
  };
});
