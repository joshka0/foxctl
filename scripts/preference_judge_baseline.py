#!/usr/bin/env python3
import argparse
import json
import math
import random
import re
from collections import Counter
from pathlib import Path


TOKEN_RE = re.compile(r"[a-z0-9_./+-]+")


def read_jsonl(path: Path):
    rows = []
    text = path.read_text(encoding="utf-8")
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        rows.append(json.loads(line))
    return rows


def tokenize(text: str):
    return TOKEN_RE.findall((text or "").lower())


def length_bucket(n: int):
    if n <= 16:
        return "0_16"
    if n <= 32:
        return "17_32"
    if n <= 64:
        return "33_64"
    if n <= 128:
        return "65_128"
    return "129_plus"


def overlap_bucket(n: int):
    if n <= 1:
        return "0_1"
    if n <= 3:
        return "2_3"
    if n <= 6:
        return "4_6"
    return "7_plus"


def select_model_outputs(example):
    chosen = {
        item.get("model"): item.get("output", "")
        for item in example["chosen"].get("outputs_by_model", [])
        if item.get("model") and not item.get("error") and item.get("output")
    }
    rejected = {
        item.get("model"): item.get("output", "")
        for item in example["rejected"].get("outputs_by_model", [])
        if item.get("model") and not item.get("error") and item.get("output")
    }
    common = sorted(set(chosen) & set(rejected))
    if common:
        model = common[0]
        return model, chosen[model], rejected[model]
    return None, "", ""


def candidate_features(question, context, target_response, prompt, output, feature_mode):
    query_tokens = tokenize(question + " " + context)
    query_set = set(query_tokens)
    target_tokens = tokenize(target_response)
    target_set = set(target_tokens)
    prompt_tokens = tokenize(prompt)
    output_tokens = tokenize(output)
    output_set = set(output_tokens)
    prompt_set = set(prompt_tokens)

    feats = Counter()
    feats["bias"] = 1.0

    if feature_mode != "output_only":
        for tok, count in Counter(prompt_tokens).items():
            feats[f"p:{tok}"] = min(count, 2)
    for tok, count in Counter(output_tokens).items():
        feats[f"o:{tok}"] = min(count, 3)

    feats[f"outlen:{length_bucket(len(output_tokens))}"] = 1.0
    if feature_mode != "output_only":
        feats[f"promptlen:{length_bucket(len(prompt_tokens))}"] = 1.0

    qo_overlap = len(query_set & output_set)
    po_overlap = len(prompt_set & output_set)
    to_overlap = len(target_set & output_set)
    feats[f"qo_overlap:{overlap_bucket(qo_overlap)}"] = 1.0
    feats[f"to_overlap:{overlap_bucket(to_overlap)}"] = 1.0
    if feature_mode != "output_only":
        feats[f"po_overlap:{overlap_bucket(po_overlap)}"] = 1.0

    if target_tokens:
        for tok, count in Counter(target_tokens).items():
            feats[f"t:{tok}"] = min(count, 2)

    lower_output = output.lower()
    if "let me" in lower_output:
        feats["style:let_me"] = 1.0
    if "```" in output:
        feats["style:codeblock"] = 1.0
    if "fix" in lower_output:
        feats["style:fix"] = 1.0
    if "implement" in lower_output:
        feats["style:implement"] = 1.0
    if "check" in lower_output:
        feats["style:check"] = 1.0
    if "review" in lower_output:
        feats["style:review"] = 1.0

    return feats


def diff_features(pos, neg):
    diff = Counter()
    for key, value in pos.items():
        diff[key] += value
    for key, value in neg.items():
        diff[key] -= value
    return {k: v for k, v in diff.items() if abs(v) > 1e-9}


def dot(weights, feats):
    return sum(weights.get(k, 0.0) * v for k, v in feats.items())


def sigmoid(x):
    if x >= 0:
        z = math.exp(-x)
        return 1.0 / (1.0 + z)
    z = math.exp(x)
    return z / (1.0 + z)


def build_pairs(rows, feature_mode):
    pairs = []
    for row in rows:
        model, chosen_output, rejected_output = select_model_outputs(row)
        if not chosen_output or not rejected_output:
            continue
        question = row["input"].get("question", "")
        context = row["input"].get("context", "")
        target_response = row["input"].get("target_response", "")
        chosen_prompt = row["chosen"].get("prompt", "")
        rejected_prompt = row["rejected"].get("prompt", "")
        chosen_feats = candidate_features(question, context, target_response, chosen_prompt, chosen_output, feature_mode)
        rejected_feats = candidate_features(question, context, target_response, rejected_prompt, rejected_output, feature_mode)
        pairs.append(
            {
                "run_id": row.get("metadata", {}).get("run_id", ""),
                "eval_case_id": row.get("input", {}).get("eval_case_id", ""),
                "model": model,
                "question": question,
                "context": context,
                "target_response": target_response,
                "chosen_prompt": chosen_prompt,
                "rejected_prompt": rejected_prompt,
                "chosen_output": chosen_output,
                "rejected_output": rejected_output,
                "chosen_feats": chosen_feats,
                "rejected_feats": rejected_feats,
                "diff": diff_features(chosen_feats, rejected_feats),
            }
        )
    return pairs


def pair_accuracy(weights, pairs):
    if not pairs:
        return 0.0, []
    correct = 0
    annotated = []
    for pair in pairs:
        chosen_score = dot(weights, pair["chosen_feats"])
        rejected_score = dot(weights, pair["rejected_feats"])
        margin = chosen_score - rejected_score
        if margin > 0:
            correct += 1
        annotated.append({**pair, "chosen_score": chosen_score, "rejected_score": rejected_score, "margin": margin})
    return correct / len(pairs), annotated


def heuristic_shorter_output(pair):
    return len(tokenize(pair["rejected_output"])) - len(tokenize(pair["chosen_output"]))


def heuristic_query_overlap(pair):
    query = set(tokenize(pair["question"] + " " + pair["context"]))
    chosen_overlap = len(query & set(tokenize(pair["chosen_output"])))
    rejected_overlap = len(query & set(tokenize(pair["rejected_output"])))
    return chosen_overlap - rejected_overlap


def heuristic_target_overlap(pair):
    target = set(tokenize(pair["target_response"]))
    chosen_overlap = len(target & set(tokenize(pair["chosen_output"])))
    rejected_overlap = len(target & set(tokenize(pair["rejected_output"])))
    return chosen_overlap - rejected_overlap


def heuristic_accuracy(pairs, scorer):
    if not pairs:
        return 0.0
    correct = 0
    for pair in pairs:
        if scorer(pair) > 0:
            correct += 1
    return correct / len(pairs)


def train_pairwise_logreg(train_pairs, val_pairs, epochs, learning_rate, l2, seed):
    weights = {}
    best_weights = {}
    best_val = -1.0
    history = []

    rng = random.Random(seed)
    working = list(train_pairs)

    for epoch in range(1, epochs + 1):
        rng.shuffle(working)
        lr = learning_rate / math.sqrt(epoch)
        for pair in working:
            margin = dot(weights, pair["diff"])
            grad_scale = sigmoid(-margin)
            for key, value in pair["diff"].items():
                current = weights.get(key, 0.0)
                weights[key] = current + lr * (grad_scale * value - l2 * current)

        train_acc, _ = pair_accuracy(weights, train_pairs)
        val_acc, _ = pair_accuracy(weights, val_pairs)
        history.append({"epoch": epoch, "train_pair_accuracy": train_acc, "val_pair_accuracy": val_acc})
        if val_acc >= best_val:
            best_val = val_acc
            best_weights = dict(weights)

    return best_weights, history


def top_weight_features(weights, limit=20):
    items = sorted(weights.items(), key=lambda kv: kv[1], reverse=True)
    return {
        "positive": [{"feature": k, "weight": v} for k, v in items[:limit]],
        "negative": [{"feature": k, "weight": v} for k, v in sorted(weights.items(), key=lambda kv: kv[1])[:limit]],
    }


def main():
    parser = argparse.ArgumentParser(description="Train a simple pairwise preference judge baseline.")
    parser.add_argument("--train", required=True)
    parser.add_argument("--val", required=True)
    parser.add_argument("--summary-out", required=True)
    parser.add_argument("--weights-out", required=True)
    parser.add_argument("--epochs", type=int, default=25)
    parser.add_argument("--learning-rate", type=float, default=0.35)
    parser.add_argument("--l2", type=float, default=1e-4)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--feature-mode", choices=["full", "output_only"], default="full")
    args = parser.parse_args()

    train_rows = read_jsonl(Path(args.train))
    val_rows = read_jsonl(Path(args.val))
    train_pairs = build_pairs(train_rows, args.feature_mode)
    val_pairs = build_pairs(val_rows, args.feature_mode)

    weights, history = train_pairwise_logreg(
        train_pairs=train_pairs,
        val_pairs=val_pairs,
        epochs=args.epochs,
        learning_rate=args.learning_rate,
        l2=args.l2,
        seed=args.seed,
    )

    train_acc, _ = pair_accuracy(weights, train_pairs)
    val_acc, val_annotated = pair_accuracy(weights, val_pairs)
    shorter_acc = heuristic_accuracy(val_pairs, heuristic_shorter_output)
    overlap_acc = heuristic_accuracy(val_pairs, heuristic_query_overlap)

    hardest = sorted([p for p in val_annotated if p["margin"] <= 0], key=lambda p: p["margin"])[:10]
    summary = {
        "created_at": __import__("datetime").datetime.utcnow().isoformat() + "Z",
        "experiment": "preference_judge_baseline_v1",
        "train_path": args.train,
        "val_path": args.val,
        "train_rows": len(train_rows),
        "val_rows": len(val_rows),
        "train_pairs": len(train_pairs),
        "val_pairs": len(val_pairs),
        "params": {
            "epochs": args.epochs,
            "learning_rate": args.learning_rate,
            "l2": args.l2,
            "seed": args.seed,
            "feature_mode": args.feature_mode,
        },
        "metrics": {
            "train_pair_accuracy": train_acc,
            "val_pair_accuracy": val_acc,
            "val_shorter_output_baseline_accuracy": shorter_acc,
            "val_query_overlap_baseline_accuracy": overlap_acc,
            "val_target_overlap_baseline_accuracy": heuristic_accuracy(val_pairs, heuristic_target_overlap),
        },
        "history": history,
        "top_features": top_weight_features(weights, limit=20),
        "hardest_val_mistakes": [
            {
                "run_id": item["run_id"],
                "eval_case_id": item["eval_case_id"],
                "model": item["model"],
                "margin": item["margin"],
                "question": item["question"][:200],
                "target_response": item["target_response"][:220],
                "chosen_prompt": item["chosen_prompt"][:160],
                "rejected_prompt": item["rejected_prompt"][:160],
                "chosen_output": item["chosen_output"][:220],
                "rejected_output": item["rejected_output"][:220],
            }
            for item in hardest
        ],
    }

    Path(args.summary_out).write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    Path(args.weights_out).write_text(json.dumps(weights, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
