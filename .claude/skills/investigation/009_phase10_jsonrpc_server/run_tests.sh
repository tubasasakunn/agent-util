#!/bin/bash
# Phase 10 JSON-RPCサーバーの自動テスト実行
set -e

cd "$(dirname "$0")/../../.."
RESULTS_DIR=".claude/skills/investigation/009_phase10_jsonrpc_server/results"

echo "=== Phase 10: JSON-RPCサーバー検証 ==="
echo ""

# 1. プロトコル型テスト
echo "--- Test 1: プロトコル型テスト ---"
go test ./pkg/protocol/ -v -count=1 2>&1 | tee "$RESULTS_DIR/01_protocol.txt"
echo ""

# 2-5. RPCパッケージ全テスト（サーバー基盤、RemoteTool、ハンドラ、E2E統合）
echo "--- Test 2-5: RPCパッケージ全テスト ---"
go test ./internal/rpc/ -v -count=1 -timeout 60s 2>&1 | tee "$RESULTS_DIR/02_rpc_all.txt"
echo ""

# 6. ビルド確認
echo "--- Test 6: CLI ビルド ---"
go build -o "$RESULTS_DIR/agent_test_binary" ./cmd/agent/ 2>&1
echo "ビルド成功: $RESULTS_DIR/agent_test_binary"
echo ""

# 7. 全テスト一括実行
echo "--- Test 7: 全パッケージテスト ---"
go test ./... -timeout 60s 2>&1 | tee "$RESULTS_DIR/03_all_tests.txt"
echo ""

echo "=== 検証完了 ==="
