//! Reference harness: runs patent's library functions on fixed inputs so the
//! Go port can be compared against them byte for byte.
use patent::model::{Match, Query, Source};
use serde::{Deserialize, Serialize};
use std::future::Future;
use std::time::Instant;

#[derive(Deserialize)]
struct Fixture {
    idea: String,
    keywords: Vec<String>,
    matches: Vec<Match>,
    sources_checked: Vec<Source>,
    #[serde(default)]
    sources_failed: Vec<Source>,
}

/// testdata/embeddings/inputs.json
#[derive(Deserialize)]
struct EmbedInputs {
    texts: Vec<String>,
    rankings: Vec<RankingInput>,
}

#[derive(Deserialize)]
struct RankingInput {
    idea: String,
    matches: Vec<MatchInput>,
}

#[derive(Deserialize)]
struct MatchInput {
    name: String,
    description: String,
}

/// testdata/embeddings/golden.json
#[derive(Serialize)]
struct Golden {
    texts: Vec<String>,
    /// Token ids from the tokenizer configured as fastembed configures it.
    token_ids: Vec<Vec<u32>>,
    /// Ranker::embed_query of each text, one at a time.
    embeddings: Vec<Vec<f32>>,
    /// rank::cosine of every pair of embeddings.
    cosines: Vec<Vec<f32>>,
    /// Ranker::rank_with, which embeds all match texts in one padded batch.
    rankings: Vec<RankingOutput>,
}

#[derive(Serialize)]
struct RankingOutput {
    idea: String,
    ranked: Vec<Scored>,
}

#[derive(Serialize)]
struct Scored {
    name: String,
    similarity: f32,
}

fn to_matches(inputs: &[MatchInput]) -> Vec<Match> {
    inputs
        .iter()
        .enumerate()
        .map(|(i, m)| Match {
            name: m.name.clone(),
            source: Source::CratesIo,
            url: format!("https://example.com/{i}"),
            description: m.description.clone(),
            popularity: None,
            similarity: 0.0,
            last_updated: None,
        })
        .collect()
}

fn read_inputs(path: &str) -> EmbedInputs {
    serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap()
}

/// testdata/patent/assess-cases.json
#[derive(Deserialize)]
struct AssessCases {
    sources_checked: Vec<Source>,
    sources_failed: Vec<Source>,
    match_sets: std::collections::BTreeMap<String, Vec<Match>>,
    cases: Vec<AssessCase>,
}

#[derive(Deserialize)]
struct AssessCase {
    name: String,
    matches: String,
    response: String,
}

/// testdata/patent/assess.json: one verdict or error per case.
#[derive(Serialize)]
struct AssessOutcome {
    name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    verdict: Option<patent::Verdict>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

/// An Llm that answers every prompt with the same response.
struct Canned(String);

#[async_trait::async_trait]
impl patent::Llm for Canned {
    async fn generate(&self, _prompt: &str) -> patent::Result<String> {
        Ok(self.0.clone())
    }

    fn label(&self) -> &str {
        "canned"
    }
}

/// `assess <assess-cases.json>`: verdict::assess on each canned response. It
/// never waits: Canned answers at once, and assess only sleeps before a
/// retry, which a successful answer never triggers.
fn assess(path: &str) {
    let input: AssessCases = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
    let query = Query { idea: "a cli to kill processes on a port".into(), keywords: vec!["kill".into(), "port".into()] };
    let outcomes: Vec<AssessOutcome> = input
        .cases
        .into_iter()
        .map(|c| {
            let matches = &input.match_sets[&c.matches];
            let llm = Canned(c.response);
            let fut = patent::verdict::assess(
                &llm,
                &query,
                matches,
                input.sources_checked.clone(),
                input.sources_failed.clone(),
            );
            let mut fut = std::pin::pin!(fut);
            let mut cx = std::task::Context::from_waker(std::task::Waker::noop());
            let std::task::Poll::Ready(result) = fut.as_mut().poll(&mut cx) else {
                panic!("{}: assess waited", c.name);
            };
            match result {
                Ok(v) => AssessOutcome { name: c.name, verdict: Some(v), error: None },
                Err(e) => AssessOutcome { name: c.name, verdict: None, error: Some(e.to_string()) },
            }
        })
        .collect();
    println!("{}", serde_json::to_string_pretty(&outcomes).unwrap());
}

/// `embed <inputs.json> <tokenizer.json>`
fn embed(inputs: EmbedInputs, tokenizer_path: &str) {
    // As fastembed's load_tokenizer sets it up: truncation to min(512,
    // model_max_length) with the default (longest-first, right) strategy.
    let mut tokenizer = tokenizers::Tokenizer::from_file(tokenizer_path).unwrap();
    tokenizer
        .with_truncation(Some(tokenizers::TruncationParams {
            max_length: 512,
            ..Default::default()
        }))
        .unwrap();
    tokenizer.with_padding(None);

    let mut ranker = patent::rank::Ranker::new().unwrap();
    let token_ids = inputs
        .texts
        .iter()
        .map(|t| tokenizer.encode(t.as_str(), true).unwrap().get_ids().to_vec())
        .collect();
    let embeddings: Vec<Vec<f32>> = inputs
        .texts
        .iter()
        .map(|t| ranker.embed_query(t).unwrap())
        .collect();
    let cosines = embeddings
        .iter()
        .map(|a| embeddings.iter().map(|b| patent::rank::cosine(a, b)).collect())
        .collect();
    let rankings = inputs
        .rankings
        .iter()
        .map(|r| {
            let q = ranker.embed_query(&r.idea).unwrap();
            let ranked = ranker.rank_with(&q, to_matches(&r.matches), 1000).unwrap();
            RankingOutput {
                idea: r.idea.clone(),
                ranked: ranked
                    .into_iter()
                    .map(|m| Scored { name: m.name, similarity: m.similarity })
                    .collect(),
            }
        })
        .collect();
    let golden = Golden { texts: inputs.texts, token_ids, embeddings, cosines, rankings };
    println!("{}", serde_json::to_string(&golden).unwrap());
}

/// `bench <inputs.json> <n> [long]`: how long patent takes to load the model and to
/// rank n match texts, cycled from the inputs.
fn bench(inputs: EmbedInputs, n: usize, long: bool) {
    // The batch is padded to its longest text, so one long description makes
    // every text cost 512 tokens; `long` keeps the one in the inputs.
    let pool: Vec<&MatchInput> = inputs
        .rankings
        .iter()
        .flat_map(|r| &r.matches)
        .filter(|m| long || m.description.len() < 1000)
        .collect();
    let matches: Vec<MatchInput> = (0..n)
        .map(|i| {
            let m = pool[i % pool.len()];
            MatchInput { name: format!("{} {i}", m.name), description: m.description.clone() }
        })
        .collect();
    let t = Instant::now();
    let mut ranker = patent::rank::Ranker::new().unwrap();
    eprintln!("load: {:.0} ms", t.elapsed().as_secs_f64() * 1e3);
    let q = ranker.embed_query(&inputs.rankings[0].idea).unwrap();
    for _ in 0..3 {
        let t = Instant::now();
        let ranked = ranker.rank_with(&q, to_matches(&matches), n).unwrap();
        eprintln!("rank {} texts: {:.0} ms", ranked.len(), t.elapsed().as_secs_f64() * 1e3);
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    match args[1].as_str() {
        "embed" => return embed(read_inputs(&args[2]), &args[3]),
        "bench" => return bench(read_inputs(&args[2]), args[3].parse().unwrap(), args.get(4).is_some_and(|a| a == "long")),
        "assess" => return assess(&args[2]),
        _ => {}
    }
    let fixture: Fixture =
        serde_json::from_str(&std::fs::read_to_string(&args[2]).unwrap()).unwrap();
    let query = Query { idea: fixture.idea, keywords: fixture.keywords };
    match args[1].as_str() {
        "prompt" => print!("{}", patent::verdict::build_prompt(&query, &fixture.matches, &fixture.sources_checked)),
        "from-data" => {
            let v = patent::verdict::from_data(&fixture.matches, fixture.sources_checked, fixture.sources_failed);
            println!("{}", serde_json::to_string_pretty(&v).unwrap());
        }
        "rank-keywords" => {
            let ranked = patent::rank::rank_by_keywords(&query, fixture.matches, 50);
            println!("{}", serde_json::to_string_pretty(&ranked).unwrap());
        }
        other => panic!("unknown command {other}"),
    }
}
