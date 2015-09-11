window.builds = {}

$(function() {
  var lastID
  var count     = 10
  var tableBody = $("table tbody")
  var alertBox  = $(".alert")
  var template  = _.template($("#row-template").html())

  var label_classes = {
    "success": "label-success",
    "pending": "label-info",
    "failure": "label-danger"
  }

  $(document).ajaxError(function(event, jqxhr, settings, error) {
    var msg = settings.type + " " + settings.url + " Error!"

    alertBox.removeClass("hide").find("p").text(msg)
  })

  window.fetch = function() {
    params = { count: count }

    if(typeof(lastID) != "undefined") {
      params.before = lastID
    }

    $.getJSON("/builds/", params, function(builds) {
      _.each(builds, function(build) {
        window.builds[build.id] = build
        lastID = build.id
        build.created_at = moment(build.created_at)
        build.label_class = label_classes[build.state]
        var row = template(build)
        tableBody.append(row)
      })
    })
  }

  window.fetch()
})

function showExplainModal(id) {
  var build    = window.builds[id]
  var template = _.template($("#explain-template").html())
  var modal    = template(build)
  $(modal).appendTo("body").modal()
}

function showFailureModal(id) {
  var build    = window.builds[id]
  var template = _.template($("#failure-template").html())
  var modal    = template(build)
  $(modal).appendTo("body").modal()
}
