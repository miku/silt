# silt without an idea: the search prompt, an open space, a new search
# that turns up a crowded one, the help.

Output prompt.gif
Source settings.tape

# A live search, not the cache's answer, so that the progress screen shows.
Env SILT_CACHE_TTL "0"

Type "silt"
Sleep 400ms
Enter
Sleep 1.5s

Type "cli that estimates how long a makefile target will take from past runs"
Sleep 600ms
Enter
Wait+Screen@60s /Matches \(/
Sleep 4s

# A new search starts with the last idea in the prompt; Ctrl+U clears it.
Type "n"
Sleep 1s
Ctrl+U
Sleep 400ms
Type "react component for infinite scroll"
Sleep 600ms
Enter
Wait+Screen@60s /Matches \(/
Sleep 4s

Type "?"
Sleep 3.5s
Type "?"
Sleep 600ms

Type "q"
Sleep 1s
