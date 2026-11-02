# The sources: which are searched when, and whether this network reaches
# them. Codeberg is left out of the check: it takes 5-15 s on its own, and
# the check prints nothing until every source has answered.

Output sources.gif
Source settings.tape

Type "silt --list-sources"
Sleep 600ms
Enter
Sleep 5s

Ctrl+L
Type "silt --check-sources --exclude codeberg"
Sleep 600ms
Enter
Wait@60s
Sleep 6s
