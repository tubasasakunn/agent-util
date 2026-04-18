#!/bin/bash
# ルーター + 単一ツール サブエージェント パターンの検証
# 仮説: ルーティングをJSON mode、実行を単一ツールに分離すればSLLMでも安定する

API_URL="http://192.168.86.200:8000/v1/chat/completions"
API_KEY="gemma4-local-api-key"
MODEL="gemma-4-E2B-it-Q4_K_M"

RESULTS_DIR="$(dirname "$0")/results"
mkdir -p "$RESULTS_DIR"

SYSTEM_PROMPT='あなたはツールルーターです。ユーザーの要求に対して、どのツールを使うべきか判断してください。\n\n利用可能なツール:\n1. read_file: ファイルの内容を読み取る。引数: path(string, 必須)\n2. write_file: ファイルに内容を書き込む。引数: path(string, 必須), content(string, 必須)\n3. shell: シェルコマンドを実行する。引数: command(string, 必須)\n4. get_weather: 天気を取得する。引数: location(string, 必須)\n\n必ず以下のJSON形式で回答してください:\n{\"tool\": \"ツール名\", \"arguments\": {引数}, \"reasoning\": \"選択理由\"}'

SYSTEM_PROMPT_WITH_NONE='あなたはツールルーターです。ユーザーの要求に対して、どのツールを使うべきか判断してください。ツールが不要な場合はtoolを\"none\"にしてください。\n\n利用可能なツール:\n1. read_file: ファイルの内容を読み取る。引数: path(string, 必須)\n2. write_file: ファイルに内容を書き込む。引数: path(string, 必須), content(string, 必須)\n3. shell: シェルコマンドを実行する。引数: command(string, 必須)\n4. get_weather: 天気を取得する。引数: location(string, 必須)\n\n必ず以下のJSON形式で回答してください:\n{\"tool\": \"ツール名またはnone\", \"arguments\": {引数またはnull}, \"reasoning\": \"選択理由\"}'

# ===== テストA: ルーター（ファイル読み取り） =====
echo "=== Test A: ルーター（ファイル読み取り要求） ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"system","content":"'"$SYSTEM_PROMPT"'"},{"role":"user","content":"sample.txtの中身を読んでください"}],"response_format":{"type":"json_object"},"temperature":0.3,"max_tokens":256}' \
  | tee "$RESULTS_DIR/testA_router_read.json" | python3 -m json.tool 2>/dev/null
echo -e "\n"

# ===== テストB: ルーター（天気要求） =====
echo "=== Test B: ルーター（天気要求） ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"system","content":"'"$SYSTEM_PROMPT"'"},{"role":"user","content":"大阪の天気は？"}],"response_format":{"type":"json_object"},"temperature":0.3,"max_tokens":256}' \
  | tee "$RESULTS_DIR/testB_router_weather.json" | python3 -m json.tool 2>/dev/null
echo -e "\n"

# ===== テストC: ルーター（ツール不要） =====
echo "=== Test C: ルーター（ツール不要な質問） ==="
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"system","content":"'"$SYSTEM_PROMPT_WITH_NONE"'"},{"role":"user","content":"1+1は何ですか？"}],"response_format":{"type":"json_object"},"temperature":0.3,"max_tokens":256}' \
  | tee "$RESULTS_DIR/testC_router_no_tool.json" | python3 -m json.tool 2>/dev/null
echo -e "\n"

# ===== テストD: 完全フロー =====
echo "=== Test D: 完全フロー（ルーター→サブエージェント→最終応答） ==="

echo "--- Step 1: ルーター ---"
ROUTER_RAW=$(curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"system","content":"'"$SYSTEM_PROMPT"'"},{"role":"user","content":"東京の天気を調べて教えてください"}],"response_format":{"type":"json_object"},"temperature":0.3,"max_tokens":256}')

ROUTER_CONTENT=$(echo "$ROUTER_RAW" | python3 -c "import json,sys; print(json.load(sys.stdin)['choices'][0]['message']['content'])" 2>/dev/null)
echo "Router output: $ROUTER_CONTENT"

TOOL_NAME=$(echo "$ROUTER_CONTENT" | python3 -c "import json,sys; print(json.load(sys.stdin)['tool'])" 2>/dev/null)
TOOL_ARGS=$(echo "$ROUTER_CONTENT" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin)['arguments']))" 2>/dev/null)
echo "Selected tool: $TOOL_NAME"
echo "Arguments: $TOOL_ARGS"

echo -e "\n--- Step 2: サブエージェント（${TOOL_NAME}のみ） ---"
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"東京の天気を調べてください"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"指定された場所の現在の天気を取得する","parameters":{"type":"object","required":["location"],"properties":{"location":{"type":"string","description":"都市名"}}}}}],"temperature":0.3,"max_tokens":256}' \
  | python3 -c "
import json,sys
d = json.load(sys.stdin)
tc = d['choices'][0]['message'].get('tool_calls')
if tc:
    print(f'tool_calls: {tc[0][\"function\"][\"name\"]}({tc[0][\"function\"][\"arguments\"]})')
    print(f'finish_reason: {d[\"choices\"][0][\"finish_reason\"]}')
" 2>/dev/null

echo -e "\n--- Step 3: ツール結果→最終応答 ---"
curl -s "$API_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"東京の天気を調べて教えてください"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_sub01","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"東京\"}"}}]},{"role":"tool","tool_call_id":"call_sub01","content":"{\"temperature\":25,\"condition\":\"曇り\",\"humidity\":60,\"wind\":\"南東 3m/s\"}"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"指定された場所の現在の天気を取得する","parameters":{"type":"object","required":["location"],"properties":{"location":{"type":"string","description":"都市名"}}}}}],"temperature":0.3,"max_tokens":512}' \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['choices'][0]['message']['content'])" 2>/dev/null

echo -e "\n"

# ===== テストE: ルーター安定性（5回） =====
echo "=== Test E: ルーター安定性（ファイル読み取り要求 x5回） ==="
for i in $(seq 1 5); do
  echo -n "Run $i: "
  curl -s "$API_URL" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $API_KEY" \
    -d '{"model":"'"$MODEL"'","messages":[{"role":"system","content":"'"$SYSTEM_PROMPT"'"},{"role":"user","content":"sample.txtの中身を読んでください"}],"response_format":{"type":"json_object"},"temperature":0.3,"max_tokens":256}' \
    | python3 -c "
import json,sys
d = json.load(sys.stdin)
content = d['choices'][0]['message']['content']
parsed = json.loads(content)
print(f'tool={parsed[\"tool\"]} args={json.dumps(parsed[\"arguments\"], ensure_ascii=False)} reason={parsed.get(\"reasoning\",\"N/A\")[:60]}')
" 2>/dev/null
done

echo -e "\n=== ルーターパターンテスト完了 ==="
