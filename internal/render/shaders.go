package render

import _ "embed"

//go:embed shaders/fullscreen.vert
var Vertex string

//go:embed shaders/overlay.frag
var Overlay string

//go:embed shaders/crt.frag
var CRT string
