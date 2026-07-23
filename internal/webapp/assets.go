package webapp

import "embed"

//go:embed all:dist
var embedded embed.FS
