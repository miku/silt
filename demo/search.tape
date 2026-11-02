# An idea from the command line in the TUI: the search, the verdict and the
# matches, a match's details, a filter, another sort order.

Output search.gif
Source settings.tape

# A live search, not the cache's answer, so that the progress screen shows.
Env SILT_CACHE_TTL "0"

Type "silt 'cli tool that kills a process on a given port'"
Sleep 600ms
Enter
Wait+Screen@60s /Matches \(/
Sleep 4s

# Details. Enter again would open the URL in the browser, so Esc.
Down@400ms 3
Sleep 800ms
Enter
Sleep 3.5s
Escape
Sleep 800ms

# Filter, keep the filter, clear it.
Type "/"
Sleep 400ms
Type@150ms "kill"
Sleep 1.5s
Enter
Sleep 1.5s
Escape
Sleep 800ms

# By popularity.
Type "s"
Sleep 3s

Type "q"
Sleep 1s
