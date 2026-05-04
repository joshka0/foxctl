#!/usr/bin/env node
const fs = require("fs");

function usage() {
  console.error("usage: score-explorer-baseline.js <dataset.jsonl> <explorer-output.json>");
  process.exit(2);
}

const [datasetPath, outputPath] = process.argv.slice(2);
if (!datasetPath || !outputPath) usage();

function readJSONL(path) {
  return fs.readFileSync(path, "utf8")
    .split(/\n/)
    .filter(Boolean)
    .map((line) => JSON.parse(line));
}

function normPath(path) {
  return String(path || "").trim().replace(/^\.\/+/, "");
}

function containsFact(output, fact) {
  const haystack = JSON.stringify(output).toLowerCase();
  return haystack.includes(String(fact || "").toLowerCase());
}

const cases = new Map(readJSONL(datasetPath).map((row) => [row.id, row]));
const output = JSON.parse(fs.readFileSync(outputPath, "utf8"));
const rows = Array.isArray(output.results) ? output.results : [];
const results = rows.map((row) => {
  const expected = cases.get(row.case_id) || {};
  const expectedPaths = (expected.expected_paths || []).map(normPath);
  const actualPaths = (row.paths || []).map(normPath);
  const actualSet = new Set(actualPaths);
  const matchedPaths = expectedPaths.filter((path) => actualSet.has(path));
  const requiredFacts = expected.required_facts || [];
  const matchedFacts = requiredFacts.filter((fact) => containsFact(row, fact));
  return {
    case_id: row.case_id,
    path_recall: expectedPaths.length ? matchedPaths.length / expectedPaths.length : 0,
    fact_recall: requiredFacts.length ? matchedFacts.length / requiredFacts.length : 0,
    matched_paths: matchedPaths,
    missed_paths: expectedPaths.filter((path) => !actualSet.has(path)),
    matched_facts: matchedFacts,
    total_paths_returned: actualPaths.length,
  };
});

const summary = {
  count: results.length,
  mean_path_recall: results.length ? results.reduce((sum, row) => sum + row.path_recall, 0) / results.length : 0,
  mean_fact_recall: results.length ? results.reduce((sum, row) => sum + row.fact_recall, 0) / results.length : 0,
};

console.log(JSON.stringify({ summary, results }, null, 2));
