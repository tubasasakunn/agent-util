#!/bin/bash
# Gemma 4 E2B Function Calling 能力テスト
# 目的: SLLMがエージェントループに耐えうるtool_callsを安定して返せるか検証

API_URL="http://192.168.86.200:8000/v1/chat/completions"
API_KEY="gemma4-local-api-key"
MODEL="gemma-4-E2B-it-Q4_K_M"

RESULTS_DIR="$(dirname "$0")/results"
mkdir -p "$RESULTS_DIR"

# ---- テスト1: 単一ツール呼び出し ----
echo "=== Test 1: 単一ツール呼び出し ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role": "user", "content": "東京の天気を教えてください"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "指定された場所の現在の天気を取得する",
          "parameters": {
            "type": "object",
            "required": ["location"],
            "properties": {
              "location": {
                "type": "string",
                "description": "都市名（例: 東京、大阪）"
              }
            }
          }
        }
      }
    ],
    "temperature": 0.3,
    "max_tokens": 512
  }' | tee "$RESULTS_DIR/test1_single_tool.json" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_DIR/test1_single_tool.json"

echo -e "\n"

# ---- テスト2: 複数ツールからの選択 ----
echo "=== Test 2: 複数ツールからの選択 ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role": "user", "content": "sample.txtの中身を読んでください"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "read_file",
          "description": "ファイルの内容を読み取る",
          "parameters": {
            "type": "object",
            "required": ["path"],
            "properties": {
              "path": {
                "type": "string",
                "description": "読み取るファイルのパス"
              }
            }
          }
        }
      },
      {
        "type": "function",
        "function": {
          "name": "write_file",
          "description": "ファイルに内容を書き込む",
          "parameters": {
            "type": "object",
            "required": ["path", "content"],
            "properties": {
              "path": {
                "type": "string",
                "description": "書き込むファイルのパス"
              },
              "content": {
                "type": "string",
                "description": "書き込む内容"
              }
            }
          }
        }
      },
      {
        "type": "function",
        "function": {
          "name": "shell",
          "description": "シェルコマンドを実行する",
          "parameters": {
            "type": "object",
            "required": ["command"],
            "properties": {
              "command": {
                "type": "string",
                "description": "実行するコマンド"
              }
            }
          }
        }
      }
    ],
    "temperature": 0.3,
    "max_tokens": 512
  }' | tee "$RESULTS_DIR/test2_multi_tool_select.json" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_DIR/test2_multi_tool_select.json"

echo -e "\n"

# ---- テスト3: ツール不要な質問（ツールを呼ばないべき） ----
echo "=== Test 3: ツール不要な質問 ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role": "user", "content": "1+1は何ですか？"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "指定された場所の現在の天気を取得する",
          "parameters": {
            "type": "object",
            "required": ["location"],
            "properties": {
              "location": {
                "type": "string",
                "description": "都市名"
              }
            }
          }
        }
      }
    ],
    "temperature": 0.3,
    "max_tokens": 512
  }' | tee "$RESULTS_DIR/test3_no_tool_needed.json" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_DIR/test3_no_tool_needed.json"

echo -e "\n"

# ---- テスト4: マルチターン（ツール結果を返した後の応答） ----
echo "=== Test 4: マルチターン（ツール結果フィードバック） ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role": "user", "content": "東京の天気を教えてください"},
      {"role": "assistant", "content": null, "tool_calls": [{"id": "call_001", "type": "function", "function": {"name": "get_weather", "arguments": "{\"location\": \"東京\"}"}}]},
      {"role": "tool", "tool_call_id": "call_001", "content": "{\"temperature\": 22, \"condition\": \"晴れ\", \"humidity\": 45}"}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "指定された場所の現在の天気を取得する",
          "parameters": {
            "type": "object",
            "required": ["location"],
            "properties": {
              "location": {
                "type": "string",
                "description": "都市名"
              }
            }
          }
        }
      }
    ],
    "temperature": 0.3,
    "max_tokens": 512
  }' | tee "$RESULTS_DIR/test4_multi_turn.json" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_DIR/test4_multi_turn.json"

echo -e "\n"

# ---- テスト5: 構造化出力（JSON mode） ----
echo "=== Test 5: 構造化出力（JSON mode） ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "'"$MODEL"'",
    "messages": [
      {"role": "user", "content": "東京タワーについて、name, height_m, built_year の3項目をJSON形式で教えてください"}
    ],
    "response_format": {"type": "json_object"},
    "temperature": 0.3,
    "max_tokens": 512
  }' | tee "$RESULTS_DIR/test5_json_mode.json" | python3 -m json.tool 2>/dev/null || cat "$RESULTS_DIR/test5_json_mode.json"

echo -e "\n"

# ---- テスト6: 安定性テスト（同じリクエストを5回投げる） ----
echo "=== Test 6: 安定性テスト（単一ツール呼び出し x5回） ==="
for i in $(seq 1 5); do
  echo "--- Run $i ---"
  curl -s "$API_URL" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    -d '{
      "model": "'"$MODEL"'",
      "messages": [
        {"role": "user", "content": "東京の天気を教えてください"}
      ],
      "tools": [
        {
          "type": "function",
          "function": {
            "name": "get_weather",
            "description": "指定された場所の現在の天気を取得する",
            "parameters": {
              "type": "object",
              "required": ["location"],
              "properties": {
                "location": {
                  "type": "string",
                  "description": "都市名"
                }
              }
            }
          }
        }
      ],
      "temperature": 0.3,
      "max_tokens": 512
    }' | tee "$RESULTS_DIR/test6_stability_run${i}.json" | python3 -c "
import json, sys
data = json.load(sys.stdin)
choice = data['choices'][0]
fr = choice.get('finish_reason', 'N/A')
tc = choice['message'].get('tool_calls')
content = choice['message'].get('content')
if tc:
    for t in tc:
        print(f'  finish_reason={fr} tool={t[\"function\"][\"name\"]} args={t[\"function\"][\"arguments\"]}')
else:
    print(f'  finish_reason={fr} content={content[:80] if content else \"null\"}')
" 2>/dev/null
done

echo -e "\n=== テスト完了 ==="
echo "結果は $RESULTS_DIR/ に保存されました"
