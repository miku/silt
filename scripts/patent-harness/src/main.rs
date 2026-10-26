//! Reference harness: runs patent's library functions on fixed inputs so the
//! Go port can be compared against them byte for byte.
use patent::model::{Match, Query, Source};
use serde::Deserialize;

#[derive(Deserialize)]
struct Fixture {
    idea: String,
    keywords: Vec<String>,
    matches: Vec<Match>,
    sources_checked: Vec<Source>,
    #[serde(default)]
    sources_failed: Vec<Source>,
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
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
