# ADR-0001: Brave Search API を採用する — gem-search での不採用判断を反転する

> ステータス: 承認済み
> 日付: 2026-09-12

## 背景

nlink-jp は 2026-04 に gem-search（util-series）で Web 検索バックエンドを選定した際、
前身 agentic-web-search で試した Brave Search API を **不採用** とした。理由は
(1) 利用規約 §3(b) の制限 — 検索結果の保存・再配布・AI 学習の禁止 — が攻撃的に
見えたこと、(2) 有償登録が要ること。代わりに Vertex AI の Google Search Grounding を
採った（gem-search RFP / architecture 参照）。

本プロジェクトは Brave Search API を使う。同じ組織が 5 か月で逆の判断をするので、
理由を残す。

## 判断

**Brave Search API（Web Search / LLM Context / Answers）を採用する。**
ToS が設計に課す制約は以下の 2 点に限り、いずれも実装で守る。

1. **検索結果をディスクに置かない。** §3(b)(i) が許すのは「運用に必要な一時的保持」
   のみ。lookup 群が持つ TTL 付きファイルキャッシュは移植しない。
2. **実応答をリポジトリに置かない。** "Search Results" の定義は Generated Results
   （Answers の回答文）と Third-Party Content を含む。テストフィクスチャは実 API で
   形式を確認した上で合成し、本文・URL・スニペットは架空値にする。

## 根拠

- **用途が違う。** gem-search は Grounding の結果を自分の LLM に食わせて再構成する
  レポート生成器で、保存・派生物の論点が設計上つきまとう。brave-search は検索
  プリミティブを返すだけで、再構成・保存・再配布のいずれも行わない。
- **ToS（2026-09-01 改定）を再読した。** 禁止は保存・キャッシュ、派生物、再配布・
  再販、AI モデルの学習・評価・改善。**推論時に LLM の入力として使うことを制限する
  条項は無く**、LLM Context / Answers はその用途のために売られている。§4 の
  "Powered by Brave" 表示は "if Customer elects to provide attribution" で任意。
- **有償登録は済んだ**（利用者決定）。心理的障壁は消えた。
- GCP を要しない検索の入口が要る。gem-search は GCP 課金基盤が前提。

## 検討した代替案

- **Vertex AI Web Grounding（gem-search の流用）** — 検索プリミティブとしては重く、
  GCP が要る。用途が違うので併存させる。
- **DuckDuckGo** — Web 検索 API が存在せず、HTML エンドポイントは robots.txt で
  `Disallow: /`。agentic-web-search ADR-001 で却下済み。再検討しない。
- **公式 brave/brave-search-skills（MIT）の流用** — 仕様の参照元としてクレジットし、
  コードは持ち込まない（コミュニティ製・スキル形式の依存を採らない組織方針）。

## 帰結

- `cache` サブコマンド・`[cache]` セクションを持たない。
- httptest / SSE のフィクスチャはすべて合成。live テストは応答本文を assert する
  だけで保存しない。
- README に「検索結果を AI モデルの学習・評価に使ってはならない」旨を明記する
  （ツールは推論時の入力に使うだけ）。
- gem-search の architecture 文書にある「Brave 不採用」の記述は歴史的記録として
  残し、本 ADR を相互参照する。
