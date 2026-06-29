# docs/demo.tape — VHS script for the README demo GIF.
# Regenerate from the repo root with:  vhs docs/demo.tape
# Runs fully offline on the deterministic `test` backend (no API key, no network, no personal data).

Output docs/demo.gif

Require go

Set Shell        "bash"
Set FontSize     12
Set Width        1280
Set Height       720
Set Padding      16
Set WindowBar    Colorful
Set Framerate    30
Set TypingSpeed  55ms

# Build + launch the TUI, hidden from the recording.
Hide
Type "cd go && go build -o /tmp/thought-demo ./cmd/thought" Enter
Sleep 25s
Type "clear" Enter
Sleep 400ms
Type "/tmp/thought-demo tui --backend test" Enter
Sleep 3500ms
Show

# Splash.
Sleep 2s

# Chat: send a goal, get the answer.
Type "What is 7 times 8?"
Sleep 700ms
Enter
Sleep 4s

# The cognition overview — the whole system at once.
Shift+Tab
Sleep 6s

# Per-subsystem tabs: Conscious (thought graph + A* search), Subconscious, Action.
Tab
Sleep 5s
Tab
Sleep 4s
Tab
Sleep 4s

# Back to the chat surface, then quit.
Shift+Tab
Sleep 2500ms
Ctrl+C
Sleep 800ms
