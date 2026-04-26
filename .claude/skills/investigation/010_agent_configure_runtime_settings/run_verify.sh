#!/usr/bin/env bash
# 010 検証スクリプト: agent.configure の実機SLLM動作確認
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$DIR/../../../.." && pwd)"

cd "$ROOT"

echo "[build] agent バイナリをビルド"
go build -o "$DIR/agent_test_binary" ./cmd/agent/

cd "$DIR"

echo "[run] sllm_configure_verify を実行"
SLLM_ENDPOINT="${SLLM_ENDPOINT:-http://localhost:8080/v1/chat/completions}" \
SLLM_API_KEY="${SLLM_API_KEY:-sk-gemma4}" \
go run ./sllm_configure_verify.go

echo "[done] 結果は results/ に保存されました"
