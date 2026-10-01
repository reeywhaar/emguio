// Package app holds what this program is, as distinct from what it was told.
//
// The line against internal/config: whether an operator can change it without rebuilding.
package app

const Name = "emguio"

const ProjectURL = "https://github.com/reeywhaar/emguio"

// ListenAddr is fixed inside the container. Remap with `docker run -p 8080:80`.
const ListenAddr = ":80"

// WebDir is where the image puts the built bundle. Fixed by the image layout, so it is what
// this program is rather than what it was told.
const WebDir = "/srv/web"

// DocsDir is where the image puts docs/, the source of what /docs serves.
const DocsDir = "/srv/docs"

// Version is stamped at link time:
//
//	-ldflags "-X emguio/internal/app.Version=$(git rev-parse --short HEAD)"
var Version = "dev"
