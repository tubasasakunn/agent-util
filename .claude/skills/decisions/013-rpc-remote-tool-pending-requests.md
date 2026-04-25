---
id: "013"
title: JSON-RPCサーバーにRemoteToolアダプタ+PendingRequestsパターンを採用
date: 2026-04-25
status: accepted
---

## コンテキスト

Phase 10でJSON-RPC over stdioサーバーを実装するにあたり、双方向通信の核心設計が必要になった。
ラッパー→コア（tool.register, agent.run）は通常のRPCだが、
コア→ラッパー（tool.execute）はサーバーがクライアントにリクエストを送る逆方向通信であり、
単一のstdin/stdoutチャネル上でリクエストとレスポンスが混在する問題がある。

## 検討した選択肢

### A. コールバック登録方式
ラッパーがtool.registerで実行ハンドラの参照を渡し、コアが直接呼び出す。
→ プロセス間通信では不可能。

### B. ポーリング方式
ラッパーが定期的にpendingキューを問い合わせる。
→ レイテンシ増大、実装複雑。

### C. RemoteTool + PendingRequests方式
tool.Toolインターフェースを実装するRemoteToolアダプタが、Execute()時にJSON-RPCリクエストをstdoutに書き出し、PendingRequestsでレスポンスを待つ。stdinのdispatch()がmethodフィールドの有無でRequest/Responseを判別し、ResponseはPendingRequestsにルーティングする。

## 判断

RemoteTool + PendingRequestsパターンを採用する。

## 理由

- tool.Toolインターフェースを変更せずにラッパー側ツールを統合できる
- Engine側は通常のツールとRemoteToolを区別する必要がない（透過的プロキシ）
- methodフィールドの有無による判別はJSON-RPC 2.0仕様と整合的
- PendingRequestsの channel ベースの待ち合わせはGoのイディオムに適合
- agent.runの排他制御（sync.Mutex）でEngine のステートフル性を保護

## 影響

- stdinから読み取ったメッセージはdispatch()で一元的に振り分けられる
- RemoteToolのIsConcurrencySafe()はfalse（fail-closed）
- agent.runは同時に1つしか実行できない
- ツールの動的登録（Engine.RegisterTool）が可能になった
