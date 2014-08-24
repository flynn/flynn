$(function() {
  var tableBody = $("table tbody")
  var alertBox  = $(".alert")
  var template  = _.template($("#row-template").html())

  $(document).ajaxError(function(event, jqxhr, settings, error) {
    var msg = settings.type + " " + settings.url + " Error!"

    alertBox.removeClass("hide").find("p").text(msg)
  })

  $.getJSON("/builds", function(builds) {
    _.each(builds, function(build) {
      build.created_at = moment(build.created_at)
      var row = template(build)
      tableBody.append(row)
    })
  })
})
