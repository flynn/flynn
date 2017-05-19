target("manifest", depends("../util/release/flynn-release"), function()
  exec(
    "../util/release/flynn-release",
    "manifest",
    "--output=bin/manifest.json",
    "--image-repository=" .. ImageRepository,
    "manifest_template.json")
end)
