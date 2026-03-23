package main

import _ "embed"

// Embedded JetBrains Mono Nerd Font covers Latin, Greek, Cyrillic, box drawing,
// powerline, devicons, and other Nerd Font glyphs.
//
//go:embed fonts/JetBrainsMonoNerdFont-Regular.ttf
var embeddedNerdFont []byte
