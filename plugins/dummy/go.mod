module github.com/denglertai/gonfig/plugins/dummy

go 1.27

require github.com/denglertai/gonfig v0.0.0

// The plugin is built against the gonfig sources it is part of. Plugins outside this repository require a
// released version instead.
replace github.com/denglertai/gonfig => ../..
