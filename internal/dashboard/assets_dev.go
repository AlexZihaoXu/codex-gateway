//go:build !production

package dashboard

import "embed"

//go:embed static
var embeddedAssets embed.FS

const embeddedRoot = "static"
