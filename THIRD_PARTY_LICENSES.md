# Third-Party Licenses

This file documents the licenses of third-party software included in or
used by hangon.

---

## JetBrains Mono Nerd Font

Embedded in the binary for terminal rendering (fonts/ directory).

- **Project:** https://github.com/ryanoasis/nerd-fonts
- **Original font:** https://github.com/JetBrains/JetBrainsMono
- **License:** SIL Open Font License, Version 1.1

See [fonts/OFL.txt](fonts/OFL.txt) for the full license text.

Copyright 2020 The JetBrains Mono Project Authors
(https://github.com/JetBrains/JetBrainsMono)

---

## Noto Sans Mono CJK SC (Subset)

Embedded in the binary (gzip-compressed) for CJK character rendering.
Subset includes CJK Unified Ideographs, Hiragana, Katakana, Hangul
Syllables, CJK Symbols, and Fullwidth Forms.

- **Project:** https://github.com/notofonts/noto-cjk
- **License:** SIL Open Font License, Version 1.1

See [fonts/NotoSansMonoCJKsc-LICENSE.txt](fonts/NotoSansMonoCJKsc-LICENSE.txt)
for the full license text.

Copyright 2014-2021 Adobe (http://www.adobe.com/)

---

## libghostty-vt (Ghostty Terminal Library)

Used at build time for the `tty` session type terminal emulator. The
library is statically linked when building with `-tags ghostty`.

- **Project:** https://github.com/ghostty-org/ghostty
- **License:** MIT License

Copyright (c) 2024 Mitchell Hashimoto, Ghostty contributors

Permission is hereby granted, free of charge, to any person obtaining a
copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the
Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
DEALINGS IN THE SOFTWARE.
