package archive

import "errors"

var errNotSupportedPlatform = errors.New("platform and architecture is not supported")

// Whiteouts are files with a special meaning for the layered filesystem.
// Docker uses AUFS whiteout files inside exported archives. In other
// filesystems these files are generated/handled on tar creation/extraction.

// whiteoutPrefix prefix means file is a whiteout. If this is followed by a
// filename this means that file has been removed from the base layer.
const whiteoutPrefix = ".wh."

// whiteoutMetaPrefix prefix means whiteout has a special meaning and is not
// for removing an actual file. Normally these files are excluded from exported
// archives.
const whiteoutMetaPrefix = whiteoutPrefix + whiteoutPrefix

// WhiteoutLinkDir is a directory AUFS uses for storing hardlink links to other
// layers. Normally these should not go into exported archives and all changed
// hardlinks should be copied to the top layer.
const whiteoutLinkDir = whiteoutMetaPrefix + "plnk"

// whiteoutOpaqueDir file means directory has been made opaque - meaning
// readdir calls to this directory do not follow to lower layers.
const whiteoutOpaqueDir = whiteoutMetaPrefix + ".opq"
