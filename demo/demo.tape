# vhs tape for the Port Mortem submission video.
#
#   vhs demo/demo.tape        -> demo/port-mortem-demo.mp4
#
# vhs (github.com/charmbracelet/vhs) spawns its own PTY and renders video
# directly, so this produces a real MP4 rather than an asciinema cast that would
# still need converting. Requires: vhs, ffmpeg, ttyd.
#
# Install on Windows:
#   winget install ffmpeg
#   go install github.com/charmbracelet/vhs@latest
#   (ttyd: scoop install ttyd, or grab the release binary)
#
# The value of a tape over a live take: the recording is REPRODUCIBLE. Re-run it
# after any change and the video regenerates identically. No retakes, no stray
# typos, no mouse in frame.

Output demo/port-mortem-demo.mp4
Output demo/port-mortem-demo.gif

Set Shell "bash"
Set FontSize 15
Set Width 1600
Set Height 900
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 40ms

# ---------------------------------------------------------------------------
# Title
# ---------------------------------------------------------------------------
Type "clear && cat demo/title.txt"
Enter
Sleep 6s

# ---------------------------------------------------------------------------
# 1. The original test suite, unmodified.
# ---------------------------------------------------------------------------
Type "clear"
Enter
Type "# 1. The ORIGINAL suite. Unmodified. TZ=UTC is required by upstream itself."
Enter
Sleep 2s
Type "cd upstream && TZ=UTC npx jest --silent 2>&1 | tail -6"
Enter
Sleep 12s

Type "# Zero tracked-file modifications:"
Enter
Sleep 1500ms
Type "git status --porcelain && cd .."
Enter
Sleep 4s

# ---------------------------------------------------------------------------
# 2. Hash manifest — makes "unmodified" checkable.
# ---------------------------------------------------------------------------
Type "clear"
Enter
Type "# 2. SHA-256 of all 24 upstream test + source files, at the pinned commit."
Enter
Sleep 2s
Type "node conformance/hash-tests.mjs"
Enter
Sleep 5s

# ---------------------------------------------------------------------------
# 3. The conformance story. This is the centrepiece.
# ---------------------------------------------------------------------------
Type "clear"
Enter
Type "# 3. Jest cannot run against a Go binary, and FFI into Node is banned."
Enter
Type "#    So: use the original suite as an ORACLE instead of translating it."
Enter
Sleep 3s
Type "cd conformance && TZ=UTC node capture.mjs && cd .."
Enter
Sleep 6s

Type "# Replay all 130 recorded call sites against the port:"
Enter
Sleep 1500ms
Type "cd port && go run ./cmd/conformance -trace ../conformance/trace.json | tail -12 && cd .."
Enter
Sleep 10s

# ---------------------------------------------------------------------------
# 4. Live differential fuzzing. Bonus criterion is 60s clean.
# ---------------------------------------------------------------------------
Type "clear"
Enter
Type "# 4. Live differential fuzz: 16 timezones, every DST width, next() + prev()."
Enter
Sleep 2s
Type "cd corpus && node fuzz.mjs 60 300 | tail -20 && cd .."
Enter
Sleep 70s

# ---------------------------------------------------------------------------
# 5. The finding.
# ---------------------------------------------------------------------------
Type "clear"
Enter
Type "cat demo/finding.txt"
Enter
Sleep 14s

Type "clear && cat demo/closing.txt"
Enter
Sleep 10s
