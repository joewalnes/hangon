# Changelog

## 2026-04-22

- Add modifier+key combos: shift-arrow, alt-letter, ctrl-arrow, ctrl-space, and more
- Fix NUL byte transmission through tmux (use load-buffer/paste-buffer for binary data)
- Add --stdin flag to `hangon send` for piping raw bytes

## 2026-04-12

- Rewrite README with tutorials and simplified examples

## 2026-04-10

- Simplify release: auto-build on every push to main
- Add Homebrew install instructions to README

## 2026-04-09

- Add GitHub Actions release workflow with auto-increment versioning

## 2026-03-16

- Split --help into topic-based system with platform-conditional macOS content
- Add build artifacts to gitignore and make clean target

## 2026-03-15

- Initial release of hangon
- Add E2E test suite
- Restructure README for users and agents
