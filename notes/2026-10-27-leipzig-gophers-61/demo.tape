# A short silt demo for the README: a search, the results, details, a
# filter, a sort. Run with make demo; it searches the live registries.
# https://github.com/charmbracelet/vhs

Output img/demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1400
Set Height 820
Set Padding 16
Set TypingSpeed 40ms

Hide
Type "clear"
Enter
Show

Type "silt --fast 'cli tool that kills a process on a given port'"
Sleep 600ms
Enter
Wait+Screen@60s /Matches \(/
Sleep 3s

Down@400ms 3
Sleep 800ms
Enter
Sleep 3s
Escape
Sleep 600ms

Type "/kill"
Sleep 2s
Escape
Sleep 600ms

Type "s"
Sleep 1.5s
Type "s"
Sleep 2s

Type "q"
Sleep 500ms
