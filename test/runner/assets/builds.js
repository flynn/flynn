$(function() {
  var lastID
  var count     = 10
  var tableBody = $("table tbody")
  var alertBox  = $(".alert")
  var template  = _.template($("#row-template").html())

  $(document).ajaxError(function(event, jqxhr, settings, error) {
    var msg = settings.type + " " + settings.url + " Error!"

    alertBox.removeClass("hide").find("p").text(msg)
  })

  window.fetch = function() {
    params = { count: count }

    if(typeof(lastID) != "undefined") {
      params.before = lastID
    }

    $.getJSON("/builds", params, function(builds) {
      _.each(builds, function(build) {
	lastID = build.id
	build.created_at = moment(build.created_at)
	var row = template(build)
	tableBody.append(row)
      })
    })
  }

  window.fetch()
})
