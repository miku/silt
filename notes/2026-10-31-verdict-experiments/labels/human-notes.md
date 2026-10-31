# The person's labels, round 1

Kept out of README.md until Claude's sheet is done, so that its labeller
doesn't see the person's labels. Fold back into the README with the
agreement.

The person's sheet (`human.txt`, finished 2026-10-31): 201 of 203 matches
labelled, 97 y, 48 p, 56 n; 11 of 12 levels (049 has none yet).

| idea | | level | y/p/n |
|---|---|---|---|
| 005 | CLI tool that kills a process on a given port | saturated | 13/2/2 |
| 016 | a vscode extension for markdown preview | saturated | 9/5/0 |
| 020 | a helm chart to deploy a kubernetes operator | vague | 16/2/0 |
| 025 | rust crate for embedded firmware | crowded | 8/8/0 |
| 034 | load environment variables from a .env file in a node app | saturated | 11/1/5 |
| 039 | a friendly command line http client for trying out apis | crowded | 2/1/15 |
| 040 | a python library that watches a directory for file changes | crowded | 4/6/5 |
| 049 | user sign up and login for a rails application | | 7/9/2 |
| 066 | a vscode extension that estimates the carbon footprint of a cloud deployment | open | 0/3/14 |
| 068 | a python library to read and write gpx track files | crowded | 4/7/6 |
| 070 | an elixir library for rate limiting with a token bucket | crowded | 9/3/4 |
| 078 | a command line tool that checks a bibliography for retracted papers | saturated | 14/3/3 |

What the labelling showed:

- **Often really good**: half the matches shown implement the idea, and for
  005 (port killers) and 078 (retraction checkers) nearly all do.
- **The partial matches have value of their own.** Several are a nearby
  project the user may not have thought of at all. That argues for showing
  them, not only the implementations.
- **Some lists are off entirely, carried by a generic word.** 039: 15 of 18
  are no ("command line", "http client": a Minecraft modpack converter, an
  API gateway, a Cardano wallet). 066: 14 of 17 are no, mostly popular
  vscode extensions with nothing about carbon.
- **Spaces are crowded with near-copies.** 14 retraction checkers for 078,
  13 port killers for 005. The pace of code publication shows: all 7 dated
  y matches of 078 were updated in 2026, most with 0 to 15 stars. The
  recording has only the date of the last update, not of creation, so the
  pace can't be measured from it.
- **The level is a judgement, not a count.** 025 (8 y) and 070 (9 y) are
  crowded, although the sheet's rule said 5 or more y make saturated.
  Decided: the labeller's level stands; `data/levels.tsv` keeps the level
  the y count gives as `from_y`, a second target.
