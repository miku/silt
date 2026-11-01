# The Map and the Machine: How Software Discovery Dies, and What Comes After

*A field note from building a tool that is forbidden from saying "it doesn't exist."*

---

## I. The one-sentence version

Software discovery is not a search problem, and it never was. It is a
*bookkeeping* problem with a 40-year backlog; the age of AI has not fixed the
bookkeeping, it has merely made the ledger easy to forge. This paper compresses
three ideas into one argument: **(1)** the honest failure mode of point search,
**(2)** why we must not let the model answer "does this exist," and **(3)** what
a real map of software — a billion-embedding map, built from the readmes of the
entire open ecosystem — would do to every question that currently gets a guess
instead of an answer.

---

## II. The honest failure mode: "I searched, therefore it doesn't exist"

Finding software was never a physics problem. It was a bookkeeping problem, and
the bookkeeping is forty years behind the shelves it describes.

Consider the world as it is, not as search engines imply it is. Fetched from a
metadata index on a single day in 2026:

| registry | named things |
|---|---|
| npm | ≈ 6.0M |
| Go modules | ≈ 2.5M |
| Docker Hub | ≈ 1.0M |
| PyPI | ≈ 945k |
| NuGet | ≈ 873k |
| Hugging Face | ≈ 773k |
| Maven Central | ≈ 734k |
| Packagist | ≈ 525k |
| crates.io | ≈ 366k |
| RubyGems | ≈ 214k |

Ten registries, ~14.3 million named, versioned, *real* things — and that is
before Conda, CRAN, Debian, Flathub, F-Droid, LuaRocks, opam, pub.dev, the Swift
Package Index, Conan, vcpkg, and fifty more. Each registry is a private card
catalog with its own vocabulary, its own quality bar, and — increasingly — its
own wall. There is no grand index uniting them. The most you can honestly say
after any single lookup is *what you happened to check.*

This is the premise of a small tool, upsweep, that a friend of mine built. It
takes a plain-English idea ("a CLI that kills a process on a given port") and
searches wherever that idea might already live — the package registries, the
forges, the marketplaces, the news that mentions tools — ranks what it finds by
semantic similarity, and returns a verdict: **Open, Crowded, Saturated.** Three
words. That is the whole product.

But the tool has a strange design rule, and the rule is the most interesting
thing about it. It is **forbidden, by its own architecture, from ever telling
the user that their idea does not exist.** Not because the developers didn't
think of it. Because the tool has only *searched* — it has never *known* — and
the difference between those two words is the entire subject of this paper.

> *"This tool can prove that something exists, but never that it doesn't — it
> only searched some sources. Every verdict is scoped to 'found in the sources
> checked,' and sources that were selected but failed are reported as such."*

That is the integrity principle, quoted from the tool's own changelog. It sounds
like hedging. It is the opposite of hedging. It is a promise that the tool will
confess its own ignorance on every single run, because the alternative — a tool
that confidently says "no one has built this" when it merely failed to *look* —
is worse than useless. It is a lie that tells you to build something that
already exists.

The evidence that this rule is necessary, not paranoid: people find software
through web search and colleagues, and general web search beats every
code-specific engine at finding a whole project to reuse. Dedicated discovery
tools are underused — not because their ranking is bad, but because their
*coverage* is bad, and nobody can tell how bad, because coverage is unstated. Ask
any search engine "is there a tool that does X" and it will answer you with the
confidence of a librarian who has never once left the building. It has no idea
what it didn't check. It cannot have an idea. It was not built to know.

A tool that says "I checked these nine sources, and here is what came back, and
here are the three I couldn't reach" is not giving you a worse answer. It is
giving you the *only* honest answer available. And that honesty is about to
become the rarest property in software.

---

## III. Don't let the model tell you the answer

Here is the most dangerous sentence in software right now:

> "There's a package for that."

Spoken by a person, it's a guess. Spoken by a model, it's a hallucination with a
supply chain attached.

The evidence is not anecdotal. When researchers generated 576,000 code samples
with 16 language models and checked which packages they referenced actually
exist, they found that at least 5.2% of the packages recommended by commercial
models — and 21.7% by open models — **were invented.** Fabricated. Never shipped,
never indexed, never a real dependency, existing only as a name the model had
seen enough times to guess. That is 205,474 unique fake package names, and every
one of them is a potential supply-chain attack: a name that a malicious actor
could squat on tomorrow. The model's suggestion of "a package for that" is not
merely wrong; it is *a hole waiting to be filled.*

And this is not a "the models will get better" problem. It is structural. A
language model is trained on rumours of packages — on descriptions, blog posts,
Stack Overflow answers, conversations — but it has **never once held a registry
in its hands.** It does not know what exists. It knows what *sounds like it
should exist.* Those are different epistemologies, and no amount of scaling
reconciles them. A model can rank, summarise, doubt, connect, and suggest. What
it cannot do — what it is *epistemically incapable* of doing — is tell you, as a
matter of fact, that something is or is not real.

So whenever your tool asks a model "does this idea already exist?", you are
asking the one question the model is structurally unequipped to answer.

The fix is not statistical. It is *architectural.* You build a cage around the
answer, and you let the model roam freely inside the cage. This is exactly what
the prior-art tool does, and the cage is worth describing in detail because it
is a small, perfect example of engineering an epistemic guarantee:

- The model is given the top matches, with their real, *fetched* similarity
  scores, popularity, and URLs.
- The model may **raise** the verdict level — from Crowded to Saturated —
  because it can weigh context the raw scores miss.
- The model may **never lower** it. The data floor is absolute. If the numbers
  say a space is saturated, no model rhetoric makes it open.
- The model is forbidden from using an enormous list of absence phrases —
  "does not exist," "no prior art," "never been built." If it somehow emits
  one, the tool replaces the sentence with a safe, scoped headline.
- The model's gaps — reasons the idea might be new — are screened; any gap
  claiming absence is dropped.

The rationale is recorded, word for word, in the tool's source:

> *"A false positive only downgrades the copy to a safe scoped headline,
> whereas a false negative is an integrity violation — so we err toward catching
> more."*

Read that twice. The team deliberately chose to make the tool *overstate*
existence rather than understate it, because a false "it exists" costs you a
moment's disappointment, while a false "it doesn't exist" costs you weeks of
building something that was already there. In an asymmetry between two lies,
they chose the cheap one on purpose — and made the cheap one safe.

And here is the most telling bug in the whole project. When they pointed the tool
at a *reasoning* model through an OpenAI-compatible API, the model spent its
entire 2,048-token budget on thought — thousands of characters of internal
reasoning, and then… nothing. Empty output. A model so busy being confident it
forgot to answer. The guardrail quietly caught it, shrugged, and returned the
data verdict anyway. The cage worked precisely at the moment the model was most
pleased with itself.

The lesson generalises far beyond this tool. In the age of AI, **the answer to
"does this exist" must never come from a model trained on rumours of things.**
It must come from a source that has actually *held* the thing. The model can
doubt, connect, explain, and advise — and then it must be caged, because the
stakes are not a wrong answer. The stakes are a wrong *kind* of answer.

---

## IV. The map of software: 15 million readmes, and the shape they make

The prior-art verdict is *point* search: you pay for an answer, one idea at a
time, and the answer is three words. It is honest, which is rare and good. But
it is also coarse, and it cannot see what it does not think to ask. Three
observed failures, all measured, all real:

- **The walled registry lies about emptiness.** PyPI's search is bot-walled, so
  the tool "always fails to reach it." Ask it about "a python pandas data
  pipeline" and it returns **Open** — not because the idea is new (it is the
  opposite of new) but because the single largest Python registry refused to
  answer. The verdict is a pure *coverage artefact.* Open, here, means "we
  couldn't find much," not "this is open space."
- **The embedding model lies about the world.** The similarity model,
  all-MiniLM-L6-v2, is English-only. Ask about the same idea in Chinese —
  端口杀手命令行工具, "a port-killer CLI" — and the tool returns **Open**, while
  the identical idea in English comes back Saturated. An empty map is not
  emptiness. It is a hole in the index.
- **The verdict is coarse by construction.** Three thresholds on one symmetric
  sentence-similarity model, calibrated once and with no written record of how.
  "Three levels are fine as an exit code," the notes admit, "but you can't rank
  ideas by it or compare areas."

Point search is a verdict you pay for per query. What software discovery
actually wants — what it has always wanted, and what every false-negative has
been silently buying us — is a **map.** Not an answer. A map. A thing you can
walk.

So let's take the design note at its word. The design note for this project,
written before any of the tooling, says it in one line: *"we index millions of
projects and build a map that you can navigate with plain text."* The map was
the vision from the start. The verdict was just the first thing that got built.
Here is what the map actually requires — and it is now, for the first time,
cheap enough to build.

**The pitch, concretely.** Take every package repository in the open ecosystem —
call it fifteen million readmes and repository descriptions, the full text of
what software *says it is* — and run an embedding model over all of it.
fifteen million vectors in a modest-dimensional space (all-MiniLM gives 384
dimensions; a good multilingual model a few more). Cluster them. Build the
neighborhood graph. That is not a metaphor; that is a computable object. It is
the *territory*, finally drawn, instead of the card catalog.

The tool already knows how to do the hard part. It runs a transformer in a
static binary — a hand-written forward pass in pure Go, with an AVX2/FMA
assembly kernel for the matrix products, accurate to ~7e-7 against the reference
implementation, fast enough to embed and rank hundreds of texts in under a
second. The heavy lifting of the map — embedding millions of documents — is the
same operation, just done a million times. This is not a fantasy; it is the
project's existing engine pointed at a bigger input.

What does a map let you do that a verdict cannot?

**First: see the holes.** A verdict tells you a space is Open. A map shows you
*which* spaces are genuinely unexplored and which are merely unreachable. The
walled PyPI and the English-only embedding stop being silent lies and become
visible empty regions — and you can tell them apart, because the map carries
provenance: *which source, when, and whether it was actually reached.* Coverage
gaps stop being hidden; they become the most interesting thing on the page.

**Second: ask questions in bulk.** A verdict is one question. A map is a
thousand questions — walk the neighborhood of an idea, find servers, compare
adjacent spaces, ask "what's missing here" the same way you ask "what's here."
The notes put it exactly right: *"a fast, deterministic score could be run over
many ideas… to find underserved areas, without an LLM call per query."* The
verdict has an audience of one. The map has an audience of everything.

**Third — and this is where the essay turns — the map is what survives the age
of AI.** Because here is the thing nobody is saying clearly: **in the age of
cheap code generation, the map is no longer constrained by the world.**
Somebody can now build an *artificial* software index. Write down every idea.
Describe it. Generate the package on the fly, the moment it's asked for. The map
no longer describes its territory — it **creates** it. That inverts discovery's
oldest axiom — that the map reflects what exists — and it changes the meaning of
discovery in a way worth sitting with.

---

## V. Search becomes delegation, and the map becomes the only honest thing

Two forces, both new, are converging on software discovery, and they pull in
opposite directions. The first is **delegation.** The second is **fabrication.**
The map is the thing that survives both.

### Delegation: nobody searches anymore

With cheap code generation, the human leaves the loop. You don't ask "does this
exist"; you tell an agent "I need X" and it figures out how. Discovery stops
being a *moment* — "let me see if this exists" — and becomes a *capability*:
"whenever you need X, find it or build it." The searcher is no longer a person
with a sense of taste. The searcher is a program that gates on an exit code.

This has a concrete consequence: **the agent has weak incentive to search
honestly.** If generating a replacement is cheaper than finding the existing
thing, the agent will happily tell you "here's a fresh one" and never once
bother to check whether a fresh one was necessary. The cheapest path to
"solved" is often "just build another." Search, for an agent, is overhead.
Honest search is *enforced* on a searcher that would rather not search — the
way a linter enforces style on a compiler that doesn't care.

That is why the three-word verdict is the right shape for the future, not a
compromise with it. Open, Crowded, Saturated. One byte. It survives being read
by software. When the audience is an agent, a human-readable paragraph of nuance
is a liability; a single machine-checkable floor above which the answer cannot
lie is a *contract.* The prior-art tool, by accident, built the one thing the
age of delegation needs most: a verdict that a program can trust, because it is
rigged to err on the side of "it exists, keep looking."

### Fabrication: the map that creates its own territory

And then there is the artificial index. Once code is cheap, the ledger stops
needing to reflect reality. You can write down an idea and generate the package
the moment it's demanded. Every idea, described, ready to be built. A manufactured atlas of
possibilities.

Here is the surprising part: **this does not end discovery. It changes its
meaning in a precise way.** Three consequences:

1. **Crowding becomes a property of ideas, not of shipped code.** A space can
   be saturated even where nothing has shipped — because "someone could build
   this in an afternoon" becomes a real signal. The verdict, Open/Crowded/
   Saturated, is already measuring the *shape of an idea-space*, not the count
   of artifacts in it. It ports over to the artificial index almost unchanged.
2. **The catalog of possibilities is exactly what the verdict already is** —
   just measured over generated instances instead of real ones. The question
   "what shape of thing exists in this idea-space" does not go away when the
   instances are generated. It becomes *more* important, because the map is now
   the only place the shape is visible at all.
3. **The integrity rule grows stronger, not weaker.** In an index that can
   fabricate its own landmarks, "I actually saw it" is the *one thing
   generation cannot forge.* Provenance — which source, when, and
   **real-or-made** — stops being a nice-to-have. It becomes the entire trust
   model. The tool that says "I checked these nine, here's what came back" is
   no longer a worse search. It is **the only honest contract we have left**
   once everything else is free to hallucinate.

So the two forces converge on the same answer. Delegation says the *searcher*
becomes an agent that would rather not search; the map is the thing the agent
can be held to. Fabrication says the *index* becomes something that can lie;
the map — a map with provenance attached to every landmark — is the thing that
can be checked. Point search gave you a verdict you could trust but only one at
a time. The map gives you a whole territory you can trust, and ask a thousand
honest questions of, and hand to an agent as the ground truth it is no longer
inclined to consult.

---

## VI. Toward an honest map

What would we actually build? A concrete sketch, in the spirit of the project —
small, fast, honest, and static.

- **Harvest the surface, not the code.** Package-registry metadata, readmes,
  and repository descriptions — the text that says *what software is* — not the
  full source. Fifteen million documents, each a few hundred words. Software
  Heritage shows it can be done at planetary scale; libraries.io and the rest
  show the metadata layer is the cheaper place to start.
- **Embed with provenance.** Every vector carries where it came from and when
  it was harvested. Real readme vs. generated placeholder. The embedding engine
  already exists and already runs in a static binary; it is the same forward
  pass, pointed at a bigger corpus.
- **Cluster and connect.** Neighborhood graphs over the vectors. Facets —
  language, license, domain, kind of artifact — as the controlled vocabulary
  that keeps fifteen million vectors walkable without collapsing into noise.
- **Resolve identity.** The same project lives in a dozen registries under
  three names. Normalizing repository URLs is how the map stops counting the
  same thing five times — the quiet problem that makes every naive index lie.
- **Make the holes visible.** Empty regions rendered as empty, not as "not
  found." Provenance renders coverage as a map of its own: here is what we
  actually reached; here is what we couldn't; here is what is genuinely open.

Then ask of it what a verdict can never answer: *where is no one building
things?* Because in the age of AI, that is the question that matters. Not "does
this exist" — the model can always *make* it exist. But "what shape of thing is
still genuinely open, and can be told apart from the places we just didn't
check?" The tool that can answer that question honestly is not a better search
box.

It is the only honest map of a territory that was never going to stop growing.

---

*All the numbers, the fetch of the registry counts, the measured Open/Crowded/
Saturated artefacts, the embedding accuracy figures, and the design-document
citations are drawn from the project's own notes and measurements, recorded
in its repository as it was built. The argument papers are cited in the reading
list below.*

**Reading list.** Cox, *Surviving Software Dependencies* (2019); Hucka &
Graham, *Software search is not a science* (2018); Sim *et al.*, *How Well Do
Search Engines Support Code Retrieval* (2011); Spracklen *et al.*, *We Have a
Package for You!* (2025); Latendresse *et al.*, *Is ChatGPT a Good Software
Librarian?* (2024); Prieto-Díaz, *Faceted Classification* (1991); Di Cosmo &
Zacchiroli, *Software Heritage* (2017); Pietri *et al.*, *Software Heritage
Graph Dataset* (2019); Decan *et al.*, *Dependency Network Evolution* (2019).
