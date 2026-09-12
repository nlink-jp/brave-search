# RFP: brave-search

> Generated: 2026-09-12
> Status: Draft (rev 2 — 独立検証 16 件を反映)

## 1. Problem Statement

nlink-jp のエージェント（Claude Code / gem-agent）と人間のオペレーターには、
**利用規約上クリーンな汎用 Web 検索の入口**が 1 本しかない。gem-search は
Vertex AI の Google Search Grounding を使うエージェンティック調査 CLI で、
固定 3 フェーズ（Survey → Deep-dive → Verify）の「調査レポート生成器」である。
「このクエリの上位 10 件の URL とスニペットが欲しい」「この質問に出典付きで
一言答えて欲しい」という**検索プリミティブ**の用途には重すぎ、GCP 課金基盤の
無い環境では動かない。

brave-search は Brave Search API の 3 エンドポイント — **Web Search**（順位付き
結果とスニペット）、**LLM Context**（LLM 向けに抽出済みの本文チャンク）、
**Answers**（Brave 側で検索・接地された回答と引用。単発モードと多段の research
モード）— を、CLI と MCP サーバの両面で提供する。Brave の API キーだけで動き、
GCP を要しない。検索結果を保存・再配布しない（Brave ToS §3(b)）。

利用者は自分（nlink-jp のエージェント運用・調査業務）。Brave の Search / Answers
両プランは契約済み。

## 2. Functional Specification

### Commands / API Surface

```
brave-search web <query>              # Web Search: 順位付き結果
brave-search context <query>          # LLM Context: 接地用チャンク
brave-search answer <question>        # Answers 単発モード（引用付き）
brave-search research <question>      # Answers research モード（多段検索）
brave-search auth check               # API キーの有効性とプラン確認（唯一の検証点）
brave-search mcp                      # MCP サーバとして起動（stdio）
brave-search --version                # 版数（brew test が叩く）
```

### パラメータ行列（CLI / MCP / config / 上流の唯一の対応表）

CLI フラグ・MCP 引数・config キーは**この 1 表から導出**する。表に無い組み合わせは
存在しない。優先順位は **フラグ > 環境変数 > 設定ファイル > 組み込み既定**。

| 項目 | CLI | MCP 引数 | config | 上流パラメータ | 対象 | 範囲 / 既定 |
|---|---|---|---|---|---|---|
| クエリ | 位置引数 | `query`（web/context）/ `question`（answer/research） | — | `q` / `messages[0].content` | 全部 | web/context は 1–400 字・50 語以内 |
| 件数 | `--count` | `count` | `[search] count` | `count` | web / context | web 1–20、context 1–50。既定 10 |
| ページ | `--offset` | `offset` | — | `offset` | web | 0–9。既定 0 |
| 国 | `--country` | `country` | `[search] country` / `[answers] country` | `country` | 全部 | 2 文字 or `ALL`。既定 `US` |
| 言語 | `--lang` | `search_lang`（web/context）/ `language`（answer/research） | `[search] search_lang` / `[answers] language` | `search_lang` / `language` | 全部 | 既定 `en` |
| 鮮度 | `--freshness` | `freshness` | — | `freshness` | web / context | `pd`/`pw`/`pm`/`py`/`YYYY-MM-DDtoYYYY-MM-DD` |
| セーフサーチ | `--safesearch` | `safesearch` | `[search] safesearch` / `[answers] safesearch` | `safesearch` | 全部 | `off`/`moderate`/`strict`。既定 `moderate` |
| 追加抜粋 | `--extra-snippets` | `extra_snippets` | — | `extra_snippets` | web | bool。既定 false |
| 全フィールド | `--full` | **無し**（MCP は常にコンパクト） | — | — | web | 既定で落とす上流フィールドを通す |
| トークン予算 | `--max-tokens` | `max_tokens` | `[context] max_tokens` / `[answers] max_tokens` | `maximum_number_of_tokens` / `max_completion_tokens` | context / answer | context 1024–32768 既定 8192。answer は既定 API 任せ |
| URL 数 | `--max-urls` | `max_urls` | `[context] max_urls` | `maximum_number_of_urls` | context | 1–50。既定 20 |
| research クエリ数 | `--max-queries` | `max_queries` | `[answers] research_max_queries` | `research_maximum_number_of_queries` | research | 1–50。既定 **10**（API 既定 20 より絞る） |
| research 反復 | `--max-iterations` | `max_iterations` | `[answers] research_max_iterations` | `research_maximum_number_of_iterations` | research | 1–5。既定 **2**（API 既定 4） |
| research 秒 | `--max-seconds` | `max_seconds` | `[answers] research_max_seconds` | `research_maximum_number_of_seconds` | research | 1–300。既定 **120**（API 既定 180） |
| 期限 | `--timeout` | **無し** | `[api] timeout` | — | 全部 | 既定 30s。research は `max_seconds + 30s` を自動導出 |
| API 版 | — | — | `[api] api_version` | `Api-Version` ヘッダ | 全部 | 実装時点の日付を固定。既定は実装日 |
| 設定 / 出力 | `--config`, `--json` | — | — | — | 全部 | |

research の per-query トークン上限（`research_maximum_number_of_tokens_per_query`）
は Phase 1 では露出せず API 既定に任せる。

**MCP ツール**（CLI と同じ engine を共有し、同じ入力に同じ答えを返す。引数は
上の行列の「MCP 引数」列で、対象列がそのツールを含む行）:

| ツール | 引数 | 返すもの |
|---|---|---|
| `web_search` | `query`, `count`, `offset`, `country`, `search_lang`, `freshness`, `safesearch`, `extra_snippets` | 結果配列（title / url / description / age / language / extra_snippets）+ `query` メタ（original / altered）+ `more_results_available` + `meta`（コスト・残枠） |
| `llm_context` | `query`, `count`, `country`, `search_lang`, `freshness`, `safesearch`, `max_tokens`, `max_urls` | `grounding.generic[]`（url / title / snippets[]）+ `sources` + `meta` |
| `answer` | `question`, `country`, `language`, `safesearch`, `max_tokens` | `answer` 本文 + `citations[]`（number / url / snippet / 位置）+ `usage`（検索回数・トークン・コスト） |
| `research` | `question`, `country`, `language`, `safesearch`, `max_queries`, `max_iterations`, `max_seconds` | `answer` + `blindspots` + `citations[]` + `progress`（反復数・クエリ数・解析 URL 数）+ `usage` |
| `get_usage` | なし | 埋め込み `usage.md`（ツール参照・エラー回復表） |

### Input / Output

**既定出力（human-readable text）**:

- `web`: 順位・タイトル・URL・スニペット・鮮度（age）。spellcheck で query が
  書き換えられたときは `altered` を必ず表示する（黙って別のクエリの答えを
  出さない）。
- `context`: URL ごとにタイトルとチャンクを区切って表示。
- `answer` / `research`: 回答本文の後に番号付き出典一覧。research は
  `blindspots`（Brave が「調べ切れなかった」と申告した論点）を回答の後に必ず出す。
  research の進行中は **stderr に進捗**（反復・クエリ数・経過秒）を出す
  — 最長 300 秒の無言はハングと区別できないため。
- 全コマンド共通: 応答末尾（`--json` では `meta`）に**このリクエストのコスト**
  （Answers は `<usage>` タグ由来の内訳、Search 系は固定単価）と、
  `X-RateLimit-Remaining` を載せる。使った分がその場で見える。
  （Answers エンドポイントが `X-RateLimit-*` を返すかは Phase 1 で実測。
  返さなければ Answers の `meta` は `<usage>` 由来のコストのみ。）

**`--json`**: 上流の生 JSON ではなく、brave-search が定義する安定スキーマ。
web は既定でコンパクト（title / url / description / age / language /
extra_snippets）に絞り、`--full` で上流フィールドをそのまま通す。
**MCP は常にコンパクト形**で `full` 相当は持たない（応答予算の面）。

**応答予算の定義**: 予算は「明示キャップ」だけで構成する — `count`（web 20 /
context 50）、`max_tokens`、`max_urls`。これ以外の**暗黙の切り詰めを持たない**。
web はコンパクト形 × count ≤ 20 で有界、context は `max_tokens` で有界、
answer / research は上流の応答長で有界。サーバ側でファイルに落とすスピル機構は
持たない。

**exit 契約**（otx-lookup / malware-lookup 踏襲）:

| Code | 意味 |
|---|---|
| 0 | 照会が完了した（結果 0 件も正常な答え） |
| 1 | 上流障害・レート制限・タイムアウトで答えが得られなかった |
| 2 | 使用法エラー — 引数不正、設定不正、API キー未設定 |

### Configuration

`~/.config/brave-search/config.toml`（sectioned TOML。XDG 準拠、macOS でも
`~/.config` を探す）。キーはパラメータ行列の config 列と一致する。

```toml
[api]
api_key     = ""                                   # BRAVE_SEARCH_API_KEY
base_url    = "https://api.search.brave.com/res/v1"
api_version = "2026-09-12"                         # Api-Version ヘッダ。既定は実装日
timeout     = "30s"

[search]                    # web / context の既定
country     = "US"          # Brave の既定に合わせる。日本語検索は "JP" / "ja" を設定
search_lang = "en"
safesearch  = "moderate"
count       = 10

[context]                   # context のみ
max_tokens = 8192
max_urls   = 20

[answers]                   # answer / research の既定
country                = "US"
language               = "en"
safesearch             = "moderate"
max_tokens             = 0             # 0 = API 任せ
research_max_queries    = 10
research_max_iterations = 2
research_max_seconds    = 120
```

環境変数は `BRAVE_SEARCH_*`（`BRAVE_SEARCH_API_KEY` は Brave 公式スキルと同名
なので、既に設定している環境でそのまま動く）。API キーは設定ファイルか
環境変数のみ — フラグでは受け取らない（プロセス一覧・シェル履歴に残るため）。

### External Dependencies

- Brave Search API（`api.search.brave.com`）。Search プラン（`/web/search`,
  `/llm/context`）と Answers プラン（`/chat/completions`）。
- Go 標準ライブラリのみ。外部 Go 依存ゼロ。公式 Go SDK は存在しない。
- 共有コード（release スクリプト・sectioned-TOML リーダー・`internal/mcp` 骨格）
  は otx-lookup / data-toolbox-mcp から**ベンダリング**する（import しない）。

## 3. Design Decisions

### なぜ Brave か — gem-search での不採用判断を反転する

gem-search（2026-04）は前身 agentic-web-search で Brave を試した上で、
ToS §3(b) の制限（保存・再配布・AI 学習の禁止）と有償登録を理由に不採用とし、
Vertex AI Web Grounding を採った。今回は次の理由で反転する。

1. **用途が違う**。gem-search は「調査レポートを生成する」ツールで、Grounding
   の結果を自分の LLM に食わせて再構成する。brave-search は**検索プリミティブ**
   を返すだけで、結果の再構成・保存・再配布を一切しない。ToS の禁止事項に
   触れる操作が設計上存在しない。
2. **ToS を読み直した**（2026-09-01 改定版）。禁止は (i) 一時的な運用保持を
   超える保存・キャッシュ、(ii) 派生物、(xii) 再配布・再販、(xiii) AI モデルの
   学習・評価・改善。**推論時に LLM の入力として使うことを制限する条項は無く**、
   LLM Context / Answers はその用途のために売られている製品である。§4 の
   "Powered by Brave" 表示は "if Customer elects to provide attribution" で任意。
3. **有償登録は済んだ**（利用者決定）。心理的障壁は消えた。
4. GCP を要しない検索の入口が要る。gem-search は GCP 課金基盤が前提。

この反転は project ADR-0001 として記録する（「なぜ gem-search の判断と違うか」
を将来の自分が読めるように）。

### ToS が設計に課す構造差: ディスクキャッシュも実応答フィクスチャも持たない

lookup 群（otx / gti / abuse …）は TTL 付き JSON ファイルキャッシュを持つが、
brave-search は持たない。§3(b)(i) が許すのは "transient storage required for
operation" だけで、24 時間のディスクキャッシュはその外にある。プロセス内での
一時保持（レスポンスの組み立て・SSE の蓄積）に留める。`cache` サブコマンドも
`[cache]` セクションも作らない。

同じ理由で**テストフィクスチャに実応答を使わない**。"Search Results" の定義は
Generated Results（Answers の回答文）と Third-Party Content を含むので、実 API から
採取した本文をリポジトリに固定化すると保存（§3(b)(i)）かつ公開リポでは再配布
（§3(b)(xii)）になる。フィクスチャは実 API で**形式**（JSON の形・SSE の行構造・
タグ）を確認した上で**合成**し、本文・URL・スニペットは架空値にする。

保存してよいのは**自分の利用量**（リクエスト数・コスト）で、これは Search
Results ではない。ただし Phase 1 では応答に載せるだけに留め、台帳化は Phase 2。

### なぜ Go / stdlib only か

- シリーズ規約（単一バイナリ・4 プラットフォーム配布・署名/notarize）。
- 公式 SDK が無く、コミュニティ製ラッパーは採らない（サプライチェーンの是）。
  REST は 3 エンドポイントとも素直で、`net/http` + `encoding/json` で足りる。
- Answers の SSE は `bufio.Scanner` で `data:` 行を読むだけ。OpenAI SDK 互換で
  あることは「SDK を使う理由」ではない。
- 公式 brave/brave-search-skills（MIT）は**仕様の参照元**として README に
  クレジットし、コードは持ち込まない。

### 引用は内部で stream する

Answers の `enable_citations` と `enable_research` は `stream=true` でしか
使えない（blocking では 4xx）。利用者が求めるのは「出典付きの完成した回答」
であって逐次表示ではないので、**engine は常に stream=true で呼び、SSE を
全部読んでから 1 つの応答に組み立てる**。`<citation>` / `<usage>` /
`<blindspots>` / `<progress>` タグの解析は engine の責務で、CLI と MCP は
組み立て済みの構造だけを受け取る。逐次表示（`--stream`）は Phase 2。

### 既存 nlink-jp ツールとの補完関係

| ツール | 役割 | brave-search との関係 |
|---|---|---|
| gem-search | Vertex Grounding のエージェンティック調査（3 フェーズ・レポート出力） | 併存。「調べて報告書にする」は gem-search、「検索して素材を返す」は brave-search。research モードは重なるが、brave-search 側は Brave がサーバ側で回し GCP 不要 |
| gem-agent / Claude Code | MCP クライアント | brave-search を Web 検索ツールとして接続する主な利用者 |
| urlscan-lookup / rdns-lookup | 特定 URL・ドメインの調査 | brave-search は**不審 URL の調査ツールではない**。名前で検索はできるが、それは Brave のインデックスを読むだけ |
| mcp-tactics | MCP サーバ群の戦術書 | サーバが 1 本増えるので追随必須（Phase 3） |

### 明示的にスコープ外

- Images / Videos / News / Local (POI) / Suggest / Spellcheck エンドポイント
  （News は Phase 2 候補。他は要望が出るまで作らない）
- Rich data callback（`/web/rich`）
- Goggles、位置ヘッダ `X-Loc-*`（いずれも Phase 2 候補）
- 結果のディスクキャッシュ、結果の保存・エクスポート機能、実応答フィクスチャ（ToS）
- HTTP/SSE の MCP トランスポート（stdio のみ。data-toolbox-mcp の project
  ADR-0004 を踏襲）
- Answers の複数ターン会話（API 自体が messages 1 件しか受けない）
- 自前の再ランキング・要約・翻訳（ツールは Brave の答えを通すだけ）
- GUI

## 4. Development Plan

### Phase 1: Core

独立レビュー可能な単位で 4 つに分ける。**冒頭に live 実測を 1 回置く**
（§5・§7 の未確定 4 点: キーとプランの対応 / Answers の `X-RateLimit-*` /
対象ページ接触の有無 / research の実コスト）。

1. **Scaffold**（otx-lookup 鏡像）: Makefile / scripts / `internal/config`
   （sectioned TOML + `BRAVE_SEARCH_*`。キーはパラメータ行列と一致）/
   `internal/app`（version / help / 未実装は exit 2）/ `internal/mcp` 骨格
   （`get_usage` のみ。`notifications/cancelled` は受信して無視 — 上流は止められない）/
   `docs/{ja,en}` に本 RFP と ADR-0001。`make build` 署名込み・`go test -race` 緑。
2. **Search 系**: `internal/brave`（`/web/search`, `/llm/context` クライアント、
   `X-RateLimit-*` と `Api-Version` の扱い）/ `internal/engine` / CLI `web` `context`
   / MCP `web_search` `llm_context`。httptest + 合成フィクスチャで全経路をテスト。
3. **Answers 系**: SSE リーダーとタグ解析（`<citation>` `<usage>` `<blindspots>`
   `<progress>` `<answer>`）/ CLI `answer` `research`（stderr 進捗）/ MCP `answer`
   `research`。SSE フィクスチャは形式を実 API で確認した上で合成。
4. **auth check + エラー契約 + e2e**: 401/403/422/429 の構造化エラー
   （`missing_api_key` / `unauthorized` / `plan_not_subscribed` / `invalid_arguments` /
   `rate_limited`(details に reset 秒) / `upstream_error` / `timeout`）。`e2e` タグの
   live テスト（キー無しなら skip、**合計 20 リクエスト以内**、応答本文は assert
   するだけで保存しない）。`usage.md` のメタテスト（ツール名・引数・エラーコードが
   全部載っていること）。

### Phase 2: Features

- News Search（`/news/search`）
- Goggles（インライン規則のみ）
- `--stream`（answer / research の逐次表示）
- ローカル利用量台帳（リクエスト数・コストの月次集計。Search Results は含まない）
- 位置ヘッダ（`X-Loc-*`）
- research の per-query トークン上限の露出

### Phase 3: Release

- README.md / README.ja.md（利用者向け情報のみ。ToS の帰結 — キャッシュ無し —
  を「機能が無い理由」として 1 行で書く）、CHANGELOG、AGENTS.md（live 実測値を
  日付付きで Gotchas に）
- リリース（署名・notarize・verify-release・4 アーカイブ: linux amd64/arm64、
  darwin arm64、windows amd64）、homebrew-tap
- util-series submodule 統合、org profile、nlink-web-site カード
- **mcp-tactics 追随**（本文の行 + description の発火語の 2 面）
- knowledge 還元（SSE タグ解析・ToS 起因のキャッシュ非搭載・合成フィクスチャの
  理由。あわせて mcp-server-design.md の「仕様にキャンセル通知が無い」という
  誤記を訂正する）
- `check-org.sh` all green

## 5. Required API Scopes / Permissions

- Brave Search API の **API キー**（`X-Subscription-Token`）。OAuth・IAM 無し。
- 必要プラン: **Search**（`/web/search`, `/llm/context`）と **Answers**
  （`/chat/completions`）。両方契約済み。
- **未確定（Phase 1 冒頭で実測）**: 1 本のキーが両プランを跨いで有効か、
  プランごとにキーが分かれるか。ドキュメントは明言していない。分かれるなら
  `[api]` に `answers_api_key` を追加する（`api_key` へのフォールバック付き）。
  §1 の「API キーだけで動く」はこの意味で読む。
- `auth check` は Search と Answers を各 1 リクエストで叩き、プランの有無を
  分けて報告する。

## 6. Series Placement

Series: **util-series**
Reason: gem-search（Web 検索 CLI）と MCP サーバ群がここに居る。cli-series の
定義「外部サービスの対話的クライアント（ユーザー認証）」にも読めるが、
brave-search は対話的でなくパイプ向けの検索プリミティブで、MCP を持つ点で
data-toolbox-mcp / ask-gemini-mcp と同じ棚に置くのが自然。
cybersecurity-series ではない — IR 用途に限定せず、`*-lookup` の
「1 IoC → 1 属性」規約にも当てはまらない。

## 7. External Platform Constraints

2026-09-12 時点の公開ドキュメント・公式スキルからの転記。**Phase 1 で実測して
AGENTS.md Gotchas に日付付きで置き換える**。

**利用規約（2026-09-01 改定）**:
- §3(b)(i) 一時的な運用保持を超える Search Results の保存・キャッシュ・DB 化禁止
  → ディスクキャッシュ無し・実応答フィクスチャ無し
- §3(b)(xii) 再配布・再販・サブライセンス禁止 → エクスポート機能を作らない
- §3(b)(xiii) AI モデルの学習・評価・ベンチマーク・改善への使用禁止
  → README に明記（ツールは推論時の入力に使うだけ）
- §3(b)(v) 複数アカウント等によるレート制限回避の禁止
- "Search Results" は internet search results / Generated Results（Answers 出力）/
  Third-Party Content を含む定義 → Answers の回答文も同じ扱い
- §4 表示は任意。README に "Powered by Brave" を書くかは利用者判断

**料金**（要確認: ダッシュボードの実値が正）:
- Search: $5 / 1,000 リクエスト。LLM Context も同プラン。無料クレジット $5/月
- Answers: $4 / 1,000 検索 + $5 / 1M トークン（入出力とも）。research は API 既定
  で 1 回に最大 20 クエリ × 反復 4 回まで検索するので**1 回 $0.1 超も普通にあり得る**
  → 既定を絞る（クエリ 10・反復 2・120 秒。パラメータ行列と一致。実測後に見直す）

**レート制限**:
- 1 秒スライディングウィンドウ。Search 50 qps、Answers 2 qps（プラン既定）
- `X-RateLimit-Limit / Policy / Remaining / Reset` ヘッダ（Search 系で確認済み。
  **Answers で返るかは未確認**）。429 は成功扱いでなく課金されない。`Reset` 秒を
  待って再試行（指数バックオフ）。バルクは持たないのでペーシングは単純

**Web Search**:
- `q` 1–400 文字・50 語以内。`count` 1–20、`offset` 0–9（= 最大 200 件）
- `freshness` は `pd/pw/pm/py` または `YYYY-MM-DDtoYYYY-MM-DD`
- spellcheck 既定 on → `query.altered` を必ず表示
- `result_filter` で web 以外（news / videos / faq / infobox / discussions /
  locations）が混ざる。Phase 1 は `web` に固定
- 応答は 1 結果あたり大量の任意フィールド（schemas / product / recipe …）
  → コンパクト形が既定

**LLM Context**:
- 受け付けるパラメータ: `q`, `country`, `search_lang`, `count`(1–50), `spellcheck`,
  `freshness`, `safesearch`, `maximum_number_of_tokens`(1024–32768、既定 8192),
  `maximum_number_of_urls`(1–50), `maximum_number_of_snippets`, `context_threshold_mode`
- 応答は `grounding.generic[]` + `sources`。サイズはトークン予算に比例
  → MCP の `max_tokens` を明示キャップに

**Answers**:
- `POST /chat/completions`、`model: "brave"`、`messages` は**厳密に 1 件**
- 受け付けるパラメータ: `stream`, `country`, `language`, `safesearch`,
  `max_completion_tokens`, `enable_citations`, `enable_research`, `research_*`
- `enable_citations` / `enable_research` は `stream=true` 必須。research と
  citations は同時指定不可（research は `<answer>` 内に引用を含む）
- research 上限: クエリ 1–50、反復 1–5、秒 1–300、クエリあたり結果 1–60、
  クエリあたりトークン 1024–16384
- 推奨タイムアウト: 単発 30 秒以上、research 300 秒以上
- SSE 中のタグ: `<citation>{start_index,end_index,number,url,favicon,snippet}`、
  `<usage>{X-Request-Requests, X-Request-Queries, X-Request-Tokens-In/Out,
  各 Cost, X-Request-Total-Cost}`、research では `<queries>` `<analyzing>`
  `<thinking>`（デバッグ用・捨てる）、`<progress>`、`<blindspots>`、`<answer>`
- **キャンセル**: MCP 仕様には `notifications/cancelled` があるが、受信側は
  「cannot be cancelled」なら MAY ignore で、ベンダリング元の骨格（otx-lookup
  `internal/mcp/server.go`）は通知を受信して無視する。クライアントがどの手段で待つのをやめても**上流の
  research は完走し課金される**。`max_seconds` の既定を小さく保ち、usage.md に明記

**バージョニング**: `Api-Version: YYYY-MM-DD` ヘッダ。未指定は最新。
互換性を壊す変更はこのヘッダで区切られる → **クライアントは実装時点の日付を
固定して送る**（`[api] api_version` で上書き可）

**Answers / LLM Context が対象ページに接触するか**は未確認。Brave の
インデックスと抽出済みチャンクを返す設計と読めるが、mcp-tactics の tier 判定
（第三者照会か対象接触か）に関わるので Phase 1 で公式説明を確認し記録する。

## 実装後の訂正（2026-09-12）

実 API の実測で RFP の記述と食い違った点。以後は AGENTS.md の Gotchas が正典。

1. **キーはプランごとに別**（利用者がダッシュボードで確認）。`[api] answers_api_key` /
   `BRAVE_SEARCH_ANSWERS_API_KEY` を追加し、answer / research に**必須**とした。§5 の
   「`api_key` へのフォールバック」は撤回 — 未設定なら送信前に `missing_api_key`。
   ただし API はどちらのキーもどのエンドポイントでも受理する（Answers キーで
   `/web/search` が 200、レート方針は Answers プランのもの）。キーはエンドポイント
   固定ではなく「どのプランに課金・制限するか」の選択であり、ツールは分離を保つ。
2. **`Api-Version` の既定は空（最新）**。任意の日付（2026-09-12）は 404 "product api
   version is not found" で拒否された。「実装日を固定して送る」は撤回。
3. **不正キーは 401 ではなく 422**（`error.code = SUBSCRIPTION_TOKEN_INVALID`）。
   リクエスト不正の 422（`VALIDATION`）と同ステータスなので、エラー写像は上流コード
   優先・ステータスは後置き。`auth check` はこの区別を無課金 probe で利用する。
4. **answer 1 回 ≈ $0.054〜0.058**（1 検索 + 入力約 10,000 トークン）。§7 の料金欄は
   「検索 $4/1,000 + トークン」としか書いておらず、検索結果が入力トークンとして
   課金される規模を見落としていた。web 検索の約 10 倍。usage.md / README に明記。
5. **非英語の回答では citations が不安定で、決定的でもない**（同一質問・country 不変で
   5 回: en → 29, 29 / ja → 0, 0, 24）。非英語で引用の無い回答には `note` を付け、
   空の `citations` を「出典なし」と読ませない。
6. **Answers エンドポイントは `X-RateLimit-*` を返さない**（Search 系は返す）。
   §2「全コマンド共通で残枠を載せる」は Search 系に限る。
7. **従量課金キーの月間窓は limit 0**（`50;w=1, 0;w=2592000`）。0 は「上限なし」で
   あり「枯渇」ではない。`ResetSeconds` はその窓を無視する。
8. **結果スキーマの実体**: `llm_context` は `chunks[]`（sources を各チャンクに畳み込み）、
   `answer` の usage は `meta` に畳み込み（`searches` / `tokens_in` / `tokens_out` /
   `cost_usd`）。§2 の MCP ツール表の「`grounding.generic[]` + `sources`」「`usage`」は
   実装前の記述。
9. **クエリ長の制限（400 字・50 語）は web / context のみ**。answer / research の
   question には適用しない（§2 行列の対象列どおり。実装が一時的に広げていたのを訂正）。
10. **タグにエスケープが無い**。JSON を運ぶタグ（citation / usage / progress）は本文が
    JSON オブジェクトのときだけタグとして扱い、文字列タグ（answer / blindspots / debug）
    は区別不能のまま剥がす。既知の限界として AGENTS.md に記録。
11. **research のストリーム形式は未確認**（実測は利用者の指示待ち。1 回 ≈ $0.1）。

---

## Discussion Log

- **2026-09-12 起票**。利用者要望: Brave Web Search API と Answers API を使う
  CLI + MCP。
- gem-search の記録（Brave 不採用・ToS §3(b)）と衝突するため、2026-09-01 改定
  の ToS 本文を確認。保存・再配布・学習禁止は健在だが推論時利用の制限は無く、
  §4 表示は任意。用途（プリミティブ返却のみ）と契約済みという事実で反転を
  決定。ADR-0001 に記録予定。
- 決定 4 点（利用者）: ①Search + Answers 両プラン契約済み ②名称 brave-search
  ③Phase 1 は Web + Answers + **LLM Context**（同プラン・追加コスト無し）
  ④research モードは **Phase 1 から**入れる。
- 名称の代替案: brave-lookup（cybersecurity 命名だが用途が IR でない）、
  web-search（バックエンド非依存だが gem-search との統合含みになる）— 却下。
- 検討して採らなかったもの: 公式 OpenAI SDK 互換を理由にした SDK 利用（stdlib
  で足りる・サプライチェーンの是）、lookup 群と同じディスクキャッシュ（ToS）、
  サーバ側スピル（フリート方針で廃止済み）、`--api-key` フラグ（履歴に残る）。
- **rev 2（同日）**: 独立検証で 16 件指摘、全件採用。同型 4 件（CLI / MCP /
  config の引数不整合）の根本原因は表を 3 つ持っていたこと → パラメータ行列
  1 表に統合し他の表はそこから導出。高 2 件: (a) 実 API から採取した SSE
  フィクスチャは Search Results の保存・再配布に当たる → 合成フィクスチャに変更、
  (b) 「MCP 仕様にキャンセル通知が無い」は誤り（`notifications/cancelled` は
  存在。受信側 MAY ignore・自社骨格は未実装）→ 記述を訂正、knowledge 側の同じ
  誤記は Phase 3 で訂正。
- 未確定で Phase 1 冒頭に実測するもの: 1 キーで両プランを跨げるか /
  Answers が `X-RateLimit-*` を返すか / Answers・LLM Context が対象ページへ
  live 接触するか / research の実コスト。
