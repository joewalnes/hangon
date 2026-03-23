package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"sync"
)

// Embedded JetBrains Mono Nerd Font covers Latin, Greek, Cyrillic, box drawing,
// powerline, devicons, and other Nerd Font glyphs.
// License: SIL Open Font License 1.1 (see fonts/OFL.txt)
//
//go:embed fonts/JetBrainsMonoNerdFont-Regular.ttf
var embeddedNerdFont []byte

// Embedded Noto Sans Mono CJK SC (subset) covers CJK Unified Ideographs,
// Hiragana, Katakana, Hangul Syllables, CJK Symbols, and Fullwidth Forms.
// Stored gzip-compressed to reduce binary size (~10MB compressed, ~12MB raw).
// License: SIL Open Font License 1.1 (see fonts/NotoSansMonoCJKsc-LICENSE.txt)
//
//go:embed fonts/NotoSansMonoCJKsc-Regular-subset.otf.gz
var embeddedCJKFontGz []byte

var (
	embeddedCJKFont     []byte
	embeddedCJKFontOnce sync.Once
)

// getEmbeddedCJKFont lazily decompresses the embedded CJK font on first use.
func getEmbeddedCJKFont() []byte {
	embeddedCJKFontOnce.Do(func() {
		if len(embeddedCJKFontGz) == 0 {
			return
		}
		r, err := gzip.NewReader(bytes.NewReader(embeddedCJKFontGz))
		if err != nil {
			return
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			return
		}
		embeddedCJKFont = data
	})
	return embeddedCJKFont
}
