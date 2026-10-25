# Software discovery: a small bibliography

A side study for silt: what is known about how people find software to reuse,
how packages get chosen, and how large-scale software indexes are built.
There are 35 papers and 4 data services, grouped by theme. Each entry has a
short summary and a line on why it matters for silt. The BibTeX is in
[software-discovery.bib](software-discovery.bib); the keys are in brackets.

**How this was compiled (2026-10-25):** candidates came from title and
abstract searches on OpenAlex and arXiv. Each entry's metadata (DOI, venue,
pages) was checked against Crossref, or against arXiv for preprints. The
summaries are based on the papers' abstracts. Where no abstract was available,
they are based on the well-known content of the paper and kept general.

**If you only read five:** Cox 2019 · Hucka & Graham 2018 · Larios Vargas et al.
2020 · Di Grazia & Pradel 2023 · Pietri et al. 2019.

---

## 1. Why discovery matters, and how people search

**Cox, R. (2019). Surviving Software Dependencies.** *ACM Queue* 17(2).
[doi:10.1145/3329781.3344149](https://doi.org/10.1145/3329781.3344149) `[cox2019surviving]`
Argues that package managers have finally made software reuse routine, and that
the risk has moved from "can I find code" to "should I trust this dependency".
Proposes a checklist for evaluating a dependency before adopting it (design,
code quality, tests, maintenance, usage, security, licensing, its own
dependencies) and for living with it afterwards.
*For silt:* this is the epigraph of silt's design note. The checklist reads like
a list of facets a discovery index could surface for each project.

**Hucka, M. & Graham, M. J. (2018). Software search is not a science, even among
scientists.** *Journal of Systems and Software* 141.
[doi:10.1016/j.jss.2018.03.047](https://doi.org/10.1016/j.jss.2018.03.047) `[hucka2018software]`
A survey of scientists and engineers. Even highly trained people mostly find
software through general web search, colleagues and the literature.
Developers lean more on Stack Overflow, repositories and catalogs. The paper
also lists the selection criteria respondents care about and the obstacles
that stop them from finding software.
*For silt:* direct evidence that dedicated discovery tools are underused, so
the bar to beat is "a web search plus asking a colleague".

**Struck, A. (2018). Research Software Discovery: An Overview.** *IEEE
e-Science 2018*. [doi:10.1109/eScience.2018.00016](https://doi.org/10.1109/eScience.2018.00016) `[struck2018research]`
Maps the landscape of research-software discovery: who the agents are, their
business models, and example tools and strategies. Intended as a guide for
anyone systematically searching for existing software.
*For silt:* a taxonomy of discovery providers (registries, catalogs, archives,
publishers) to position silt against.

**Umarji, M., Sim, S. E. & Lopes, C. (2008). Archetypal Internet-Scale Source
Code Searching.** *OSS 2008*, IFIP / Springer.
[doi:10.1007/978-0-387-09684-1_21](https://doi.org/10.1007/978-0-387-09684-1_21) `[umarji2008archetypal]`
A survey of programmers that classifies web code searches along two
dimensions: the size of what is sought (block, subsystem, whole system) and
the motivation (reuse as-is, or a reference example).
*For silt:* silt sits at the "whole system, reuse as-is" end, which is the case
general code search serves worst.

**Sim, S. E., Umarji, M., Ratanotayanon, S. & Lopes, C. V. (2011). How Well Do
Search Engines Support Code Retrieval on the Web?** *ACM TOSEM* 21(1).
[doi:10.1145/2063239.2063243](https://doi.org/10.1145/2063239.2063243) `[sim2011how]`
An experiment with 36 participants comparing Google, Koders, Krugle, Google Code
Search and SourceForge. Reference examples were easier to find than components
to reuse as-is. General web search did best overall, but code-specific engines
did better when the target was a subsystem.
*For silt:* a reusable evaluation design: judge the relevance of the top 10
hits, varying target size and motivation.

**Sadowski, C., Stolee, K. T. & Elbaum, S. (2015). How Developers Search for
Code: A Case Study.** *ESEC/FSE 2015*.
[doi:10.1145/2786805.2786855](https://doi.org/10.1145/2786805.2786855) `[sadowski2015how]`
Surveys and logs from Google's internal Code Search. Developers average 5
search sessions and 12 queries a workday, mostly looking for code they partly
know already: how to use an API, what code does, why something fails, or
where it lives.
*For silt:* a reminder that code search inside a codebase and finding a
project to reuse are different tasks. silt is the second.

**Stolee, K. T., Welp, T., Sadowski, C. & Elbaum, S. (2025). 10 Years Later:
Revisiting How Developers Search for Code.** *PACMSE* 2(FSE).
[doi:10.1145/3715774](https://doi.org/10.1145/3715774) `[stolee2025ten]`
Revisits the 2015 study: three surveys with 1,945 responses, plus usage
statistics for more than 100,000 Code Search users. How often people search
has not changed, despite AI-assisted development. Adds explicit success
criteria for when search does and doesn't meet users' needs.
*For silt:* evidence that search is not displaced by LLM assistants, and a
template for defining "success" for a query.

## 2. Choosing among candidates: selection signals

**Larios Vargas, E., Aniche, M., Treude, C., Bruntink, M. & Gousios, G. (2020).
Selecting Third-Party Libraries: The Practitioners' Perspective.** *ESEC/FSE
2020*. [doi:10.1145/3368089.3409711](https://doi.org/10.1145/3368089.3409711) `[larios2020selecting]`
Library selection in practice is ad hoc and shaped by dozens of technical,
human and economic factors. Choosing wrongly costs time, money and effort,
depending on the library's role in the architecture.
*For silt:* the factor list is a ready-made schema for the metadata silt
should harvest and display next to each hit.

**Mujahid, S., Abdalkareem, R. & Shihab, E. (2023). What are the
characteristics of highly-selected packages? A case study on the npm
ecosystem.** *JSS* 198.
[doi:10.1016/j.jss.2022.111588](https://doi.org/10.1016/j.jss.2022.111588) `[mujahid2023characteristics]`
118 npm developers said highly-selected packages are well documented, have many
GitHub stars and downloads, and have no vulnerabilities. The authors then
check these perceptions against historical data from npm packages.
*For silt:* confirms which popularity and quality signals users expect to see
next to a result.

**Borges, H. & Valente, M. T. (2018). What's in a GitHub Star?** *JSS* 146.
[doi:10.1016/j.jss.2018.09.016](https://doi.org/10.1016/j.jss.2018.09.016) `[borges2018star]`
In a survey of 791 developers, three out of four look at star counts before
using or contributing to a project. Finds four growth patterns among the top
5,000 repositories, and warns that stars are inflated by promotion.
*For silt:* stars matter to users but are a social signal. Show them; don't
rank by them alone.

**Zerouali, A., Mens, T., Robles, G. & Gonzalez-Barahona, J. M. (2019). On the
Diversity of Software Package Popularity Metrics: An Empirical Study of npm.**
*SANER 2019*. [doi:10.1109/SANER.2019.8667997](https://doi.org/10.1109/SANER.2019.8667997) `[zerouali2019diversity]`
Computes 9 popularity metrics from libraries.io, npmjs.com and GitHub for 175k
npm packages, and finds they often disagree. Argues for a portfolio of
metrics rather than a single number.
*For silt:* patent puts each source's own metric (stars, downloads, votes)
into one `popularity` field. This paper is the case for keeping them separate
and labelled.

## 3. Reuse libraries, classification and tagging

**Prieto-Díaz, R. (1991). Implementing Faceted Classification for Software
Reuse.** *CACM* 34(5). [doi:10.1145/103167.103176](https://doi.org/10.1145/103167.103176) `[prietodiaz1991faceted]`
The classic proposal for classifying reusable components by several facets
with controlled vocabularies, instead of a single hierarchy.
*For silt:* facets (language, domain, platform, licence, kind of artefact)
remain the obvious way to make a large index navigable.

**Maarek, Y. S., Berry, D. M. & Kaiser, G. E. (1991). An Information Retrieval
Approach for Automatically Constructing Software Libraries.** *IEEE TSE* 17(8).
[doi:10.1109/32.83915](https://doi.org/10.1109/32.83915) `[maarek1991information]`
Builds browsable software libraries automatically from components'
natural-language documentation, using lexical affinities and clustering
rather than manual cataloguing.
*For silt:* an early example of silt's premise, where descriptions alone are
enough to organise a software collection.

**Mili, A., Mili, R. & Mittermeir, R. T. (1998). A Survey of Software Reuse
Libraries.** *Annals of Software Engineering* 5.
[doi:10.1023/A:1018964121953](https://doi.org/10.1023/A:1018964121953) `[mili1998survey]`
Surveys how reusable components were stored, classified and retrieved, from
text-based to specification-based methods.
*For silt:* useful background on what the "component library" era tried, and
why most of it didn't survive contact with scale.

**Frakes, W. B. & Kang, K. (2005). Software Reuse Research: Status and
Future.** *IEEE TSE* 31(7). [doi:10.1109/TSE.2005.85](https://doi.org/10.1109/TSE.2005.85) `[frakes2005reuse]`
A state-of-the-field overview of software reuse research and open problems.
*For silt:* frames reuse before package managers. Read alongside Cox 2019 to
see what changed.

**Kawaguchi, S., Garg, P. K., Matsushita, M. & Inoue, K. (2006). MUDABlue: An
Automatic Categorization System for Open Source Repositories.** *JSS* 79(7).
[doi:10.1016/j.jss.2005.06.044](https://doi.org/10.1016/j.jss.2005.06.044) `[kawaguchi2006mudablue]`
Categorises open-source projects automatically by applying latent semantic
analysis to identifiers in their source code, with no predefined category
set.
*For silt:* an alternative for when package descriptions are thin, at the cost
of fetching the code.

**Izadi, M., Heydarnoori, A. & Gousios, G. (2021). Topic Recommendation for
Software Repositories using Multi-Label Classification Algorithms.** *EMSE*
26(5). [doi:10.1007/s10664-021-09976-2](https://doi.org/10.1007/s10664-021-09976-2) `[izadi2021topic]`
Maps free-form GitHub topics onto the 228 curated "featured" topics. Then
trains multi-label classifiers on about 152k repositories, using
descriptions, READMEs, wiki pages and file names, to predict topics.
*For silt:* a concrete recipe for tagging harvested projects with a shared
vocabulary across ecosystems.

**Sas, C. & Capiluppi, A. (2022). Antipatterns in Software Classification
Taxonomies.** *JSS* 190. [doi:10.1016/j.jss.2022.111343](https://doi.org/10.1016/j.jss.2022.111343) `[sas2022antipatterns]`
Reviews existing software classification schemes and names 7 recurring
"antipatterns" that make them fail. Includes a case study on building a
taxonomy of software types.
*For silt:* a checklist to read before designing silt's own category map.

## 4. Code search

**Bajracharya, S., Ossher, J. & Lopes, C. (2014). Sourcerer: An Infrastructure
for Large-Scale Collection and Analysis of Open-Source Code.** *Science of
Computer Programming* 79.
[doi:10.1016/j.scico.2012.04.008](https://doi.org/10.1016/j.scico.2012.04.008) `[bajracharya2014sourcerer]`
Describes a pipeline for crawling and storing a large corpus of open-source
projects, extracting structured information from the code, and serving
search and analysis over it.
*For silt:* the architectural ancestor of a crawl–store–index system like the
one silt's harvest mode would need.

**Gu, X., Zhang, H. & Kim, S. (2018). Deep Code Search.** *ICSE 2018*.
[doi:10.1145/3180155.3180167](https://doi.org/10.1145/3180155.3180167) `[gu2018deep]`
Embeds code snippets and their natural-language descriptions into a shared
vector space, so code can be retrieved by semantic similarity to a query
rather than by keyword overlap.
*For silt:* the same idea as patent's embedding ranker, applied at function
level rather than project level.

**Husain, H., Wu, H.-H., Gazit, T., Allamanis, M. & Brockschmidt, M. (2019).
CodeSearchNet Challenge.** arXiv:1909.09436.
[doi:10.48550/arXiv.1909.09436](https://doi.org/10.48550/arXiv.1909.09436) `[husain2019codesearchnet]`
A corpus of about 6 million functions in six languages (Go, Java, JavaScript,
PHP, Python, Ruby), plus 99 natural-language queries with about 4k expert
relevance judgements, as a benchmark for semantic code search.
*For silt:* a model for building a small judged query set to evaluate silt's
ranking.

**Di Grazia, L. & Pradel, M. (2023). Code Search: A Survey of Techniques for
Finding Code.** *ACM Computing Surveys* 55(11).
[doi:10.1145/3565971](https://doi.org/10.1145/3565971) `[digrazia2023code]`
Covers 30 years of code search research: query types, query preprocessing and
expansion, indexing and retrieval, ranking and pruning, and empirical studies
of use.
*For silt:* the best single map of retrieval techniques. Query expansion is
directly relevant to patent's narrowing heuristic.

## 5. Harvesting at scale: archives, datasets, ecosystems

**Di Cosmo, R. & Zacchiroli, S. (2017). Software Heritage: Why and How to
Preserve Software Source Code.** *iPRES 2017*.
[hal-01590958](https://hal.science/hal-01590958) `[dicosmo2017software]`
The mission and design of Software Heritage: collect, preserve and share all
publicly available source code, harvested from forges, distributions and
package managers.
*For silt:* the most complete "harvest everything" precedent, with the same
"find it, then keep it" ambition as silt.

**Pietri, A., Spinellis, D. & Zacchiroli, S. (2019). The Software Heritage
Graph Dataset.** *MSR 2019*. [doi:10.1109/MSR.2019.00030](https://doi.org/10.1109/MSR.2019.00030) `[pietri2019software]`
A fully deduplicated Merkle-DAG export of the archive. At publication it
covered more than 5 billion unique files and 1 billion commits from more than
80 million projects, drawn from forges, distributions and package managers
(e.g. PyPI). It records when and where each artefact was seen, and is
available as CSV or Parquet or queryable on Athena.
*For silt:* a possible bulk source of project and origin lists, and an example
of crawl provenance ("seen at X on date Y"), which fits patent's honesty
rules.

**Ma, Y. et al. (2021). World of Code.** *EMSE* 26(2).
[doi:10.1007/s10664-020-09905-9](https://doi.org/10.1007/s10664-020-09905-9) `[ma2021world]`
Infrastructure that cross-references nearly all public version-control data
(commits, files, authors, projects), with periodically updated maps for
research workflows. It extends the MSR 2019 paper.
*For silt:* shows how to resolve project identity (forks, renames, the same
code in many places), a problem silt's dedup will run into at scale.

**Gousios, G. (2013). The GHTorrent Dataset and Tool Suite.** *MSR 2013*.
[doi:10.1109/MSR.2013.6624034](https://doi.org/10.1109/MSR.2013.6624034) `[gousios2013ghtorrent]`
An early continuous mirror of GitHub's public API data, with a description of
how it was collected and what research it enables.
*For silt:* a lesson in what mirroring a single forge through its API costs,
and in why silt's design note limits GitHub scope.

**Mockus, A. (2007). Large-Scale Code Reuse in Open Source Software.**
*FLOSS'07*. [doi:10.1109/FLOSS.2007.10](https://doi.org/10.1109/FLOSS.2007.10) `[mockus2007large]`
Across large open-source collections, more than half of all files appear in
more than one project. The most-reused components are small, though some
span hundreds of files.
*For silt:* reuse is already widespread at the file level. Package-level
discovery is the more structured part of it.

**Decan, A., Mens, T. & Grosjean, P. (2019). An Empirical Comparison of
Dependency Network Evolution in Seven Software Packaging Ecosystems.** *EMSE*
24(1). [doi:10.1007/s10664-017-9589-y](https://doi.org/10.1007/s10664-017-9589-y) `[decan2019empirical]`
Uses the libraries.io dataset to compare Cargo, CPAN, CRAN, npm, NuGet,
Packagist and RubyGems. Proposes metrics for growth, changeability,
reusability and fragility; the networks grow over time, with a minority of
packages responsible for most updates.
*For silt:* evidence that libraries.io-style cross-registry data works for
comparing ecosystems, and a first set of per-ecosystem health metrics.

**Kikas, R., Gousios, G., Dumas, M. & Pfahl, D. (2017). Structure and
Evolution of Package Dependency Networks.** *MSR 2017*.
[doi:10.1109/MSR.2017.55](https://doi.org/10.1109/MSR.2017.55) `[kikas2017structure]`
Studies the JavaScript, Ruby and Rust ecosystems. Transitive dependencies in
JavaScript grew 60% in one year, and ecosystems are increasingly vulnerable to
the removal of their most popular package.
*For silt:* dependency graphs are a discovery signal too: being depended on is
a stronger endorsement than a star.

## 6. Recommendation, and what LLMs get wrong

**Thung, F., Lo, D. & Lawall, J. (2013). Automated Library Recommendation.**
*WCRE 2013*. [doi:10.1109/WCRE.2013.6671293](https://doi.org/10.1109/WCRE.2013.6671293) `[thung2013automated]`
Recommends libraries from the ones a project already uses, combining
association-rule mining with collaborative filtering. Evaluated on 500
projects (recall@5 of 0.852).
*For silt:* "projects like yours also use…" needs no text query at all, which
makes it a natural extension once silt holds dependency data.

**Nguyen, P. T., Di Rocco, J., Di Ruscio, D. & Di Penta, M. (2020). CrossRec:
Supporting Software Developers by Recommending Third-Party Libraries.** *JSS*
161. [doi:10.1016/j.jss.2019.110460](https://doi.org/10.1016/j.jss.2019.110460) `[nguyen2020crossrec]`
A collaborative-filtering recommender that suggests third-party libraries
based on what similar open-source projects use.
*For silt:* same idea as Thung et al. The Di Rocco/Di Ruscio group has
follow-up work on popularity bias in such recommenders (arXiv 2501.10313,
not included here).

**Spracklen, J. et al. (2025). We Have a Package for You! A Comprehensive
Analysis of Package Hallucinations by Code Generating LLMs.** *USENIX
Security 2025*. [arXiv:2406.10279](https://arxiv.org/abs/2406.10279) `[spracklen2025package]`
Generated 576,000 code samples with 16 LLMs. At least 5.2% (commercial models)
and 21.7% (open models) of the packages they referenced don't exist, giving
205,474 unique hallucinated names, which is a supply-chain attack surface.
*For silt:* the strongest argument for patent's integrity principle: a
discovery tool must only report things it has actually seen in a registry.

**Latendresse, J., Khatoonabadi, S., Abdellatif, A. & Shihab, E. (2024). Is
ChatGPT a Good Software Librarian?** arXiv:2408.05128.
[doi:10.48550/arXiv.2408.05128](https://doi.org/10.48550/arXiv.2408.05128) `[latendresse2024chatgpt]`
Across 10,000 Stack Overflow questions, GPT-3.5 used third-party libraries
about 10% more often than humans did, favouring well-established ones. 14.2%
of the libraries it recommended had restrictive copyleft licences it didn't
mention, and 6.5% didn't work out of the box.
*For silt:* licence and "does it install" are signals silt can show from
registry metadata, where an LLM won't.

## 7. Methods behind patent/silt's ranking

**Reimers, N. & Gurevych, I. (2019). Sentence-BERT.** *EMNLP-IJCNLP 2019*.
[doi:10.18653/v1/D19-1410](https://doi.org/10.18653/v1/D19-1410) `[reimers2019sentence]`
Fine-tunes BERT in a siamese setup so that sentence embeddings can be compared
with cosine similarity. This makes large-scale semantic similarity search
practical.
*For silt:* the method behind `all-MiniLM-L6-v2`, the sentence-transformers
model patent uses.

**Wang, W. et al. (2020). MiniLM: Deep Self-Attention Distillation for
Task-Agnostic Compression of Pre-Trained Transformers.** *NeurIPS 2020*.
[arXiv:2002.10957](https://arxiv.org/abs/2002.10957) `[wang2020minilm]`
Compresses large transformers into small students by distilling the teacher's
last-layer self-attention behaviour.
*For silt:* explains why a 6-layer, 384-dimension model is good enough to run
locally, and is relevant to the embedding-backend decision (D2) in `todo.md`.

## 8. Data sources and services (not papers)

- **Libraries.io** `[librariesio]` (https://libraries.io): cross-registry
  package metadata index. Decan et al. and Zerouali et al. used its data.
  Candidate bulk source for silt.
- **ecosyste.ms** `[ecosystems]` (https://ecosyste.ms): open services for
  package, repository and dependency metadata. Its homepage returned HTTP 402
  to a plain `curl`, so check its access terms before relying on it.
- **deps.dev / Open Source Insights** `[depsdev]` (https://deps.dev): dependency
  graphs and metadata across major registries.
- **Software Heritage archive** `[swharchive]`
  (https://archive.softwareheritage.org): see §5.

---

## Takeaways for silt

1. **The bar is low but sticky.** People mostly find software through web
   search and colleagues (Hucka & Graham; Sim et al.). silt has to be clearly
   faster or more trustworthy than a web search, not just more complete.
2. **"Project to reuse" is its own search task**, distinct from code search
   (Umarji et al.; Sadowski et al.). Most retrieval research targets functions
   or snippets. Little targets packages.
3. **Show signals, don't merge them.** Selection is multi-factor (Larios Vargas
   et al.; Mujahid et al.), and popularity metrics disagree (Zerouali et al.;
   Borges & Valente). This argues against a single `popularity` number. Keep
   labelled per-source signals: stars, downloads, dependents, last release,
   licence.
4. **Classification is a trap and also necessary.** Facets (Prieto-Díaz) and
   learned tags (Izadi et al.) work. Taxonomies tend to fail in predictable
   ways (Sas & Capiluppi).
5. **Harvesting has precedents and costs.** Software Heritage, World of Code
   and GHTorrent show what full mirroring costs. Package-registry metadata
   (Decan et al.; libraries.io; deps.dev) is the cheaper layer silt's design
   note already favours.
6. **Integrity is a real differentiator.** LLMs invent packages (Spracklen et
   al.) and leave out licence caveats (Latendresse et al.). patent's rule of
   only reporting what a source returned is well-founded, and should carry
   over to silt's index as provenance: which source, and when it was
   harvested.

## Possible next reads (not verified or summarised here)

- Popularity bias in library recommenders (Di Sipio et al. 2025,
  arXiv:2501.10313).
- GitRanking: a ranking of GitHub topics for software classification (Sas et
  al. 2022, arXiv:2205.09379).
- Software mentions in scholarly papers as a discovery channel (Druskat et al.
  2024, arXiv:2402.14602; Istrate et al. 2022, arXiv:2209.00693).
- The Software Heritage open science ecosystem (Di Cosmo & Zacchiroli 2023,
  arXiv:2310.10295).
