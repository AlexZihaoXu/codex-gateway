//go:build production

package dashboard

import "embed"

//go:embed dist
var embeddedAssets embed.FS

const embeddedRoot = "dist"
