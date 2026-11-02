# --json for scripts: the verdict and the matches, picked apart with jq.

Output json.gif
Source settings.tape
Require jq

# A live search, not the cache's answer.
Env SILT_CACHE_TTL "0"

# Somewhere to write matches.json that isn't the repository.
Hide
Type `cd "$(mktemp -d)" && clear`
Enter
Show

Type "silt --json --limit 8 'react component for infinite scroll' > matches.json"
Sleep 600ms
Enter
Wait@60s
Sleep 2s

Type "jq .verdict matches.json"
Sleep 600ms
Enter
Wait
Sleep 4s

Type `jq -r '.matches[] | "\(.similarity*100|floor)  \(.source)  \(.name)"' matches.json`
Sleep 600ms
Enter
Wait
Sleep 5s
