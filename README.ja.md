# brave-search

**Brave Search API — Web 検索結果・LLM 向けページコンテキスト・出典付き回答 — を CLI と ローカル MCP サーバとして。**

brave-search は [Brave Search API](https://brave.com/search/api/) の 3 エンドポイントを提供します。

| コマンド / ツール | エンドポイント | 得られるもの |
|---|---|---|
| `web` / `web_search` | Web Search | タイトル・URL・スニペット・鮮度付きの順位付き結果 |
| `context` / `llm_context` | LLM Context | トークン予算に収まるよう抽出済みのページ本文（モデルの接地用） |
| `answer` / `answer` | Answers | 1 回の検索に基づく出典付き回答 |
| `research` / `research` | Answers（research モード） | 多段・複数検索による出典付き回答と、調べ切れなかった論点の申告 |

これは検索プリミティブです。Brave が返したものをそのまま返し、そのコールのコストを表示し、結果を保存・再構成・再配布しません。[gem-search](https://github.com/nlink-jp/gem-search) が Vertex AI 上のエージェンティックなレポート生成器であるのに対し、こちらは Brave の API キーだけで動く検索呼び出しです。

> **Status: 開発中。** 全コマンドを実装し、実 API で実測済み。

## インストール

```bash
make build  # → dist/brave-search
```

## 使い方

```bash
# 順位付き結果 — クエリは引用符なしでもよく、フラグは後ろに置ける
brave-search web go generics --count 5
brave-search web -- go -tutorial                    # 先頭が "-" の語（除外演算子）は "--" の後ろに
brave-search web "site:go.dev generics" --freshness pm --extra-snippets
brave-search web --json go generics --full          # 上流の全フィールドを JSON で

# モデルの接地用に、トークン予算に収めたページ本文
brave-search context "how do go generics work" --max-tokens 2048 --max-urls 5

# 国・言語・セーフサーチはすべてのコマンドに効く
brave-search web 生成AI --country jp --lang ja

# 1 回の検索に基づく出典付き回答
brave-search answer "what changed in go 1.25"

# research モード: 複数回・複数反復の検索と、Brave が申告する「調べ切れなかった論点」。
# 高価なコマンド — 進捗は stderr に出る
brave-search research "compare alpha and beta for use case X" --max-queries 5 --max-iterations 1

# MCP サーバとして（stdio） — ツール: web_search, llm_context, answer, research, get_usage
brave-search mcp
```

すべての結果の末尾に、そのコールのコストと残りのレート枠が出ます:

```
cost: $0.0050 (list-price estimate) · requests: 1 · rate budget left: 0/1, 1999/2000
```

Search 系エンドポイントは Brave の公表単価でリクエストごとに課金されるため、この数字は見積もりです。Answers 系は正確なコストを報告します:

```
cost: $0.0157 (reported by Brave) · requests: 1 · searches: 2 · tokens: 1234 in / 300 out
```

**answer 1 回のコストは web 検索の約 10 倍です。** Brave は検索結果をモデルに入力トークンとして食わせて課金するため、`answer` 1 回の実測は入力約 10,000 トークン・$0.054〜0.058 でした。**非英語の回答では引用（citations）が不安定です**（同じ質問で実測: 英語は毎回返り、日本語は 3 回中 1 回）。引用の無い非英語の回答には `note` でその旨が付くので、出典が要るときは再試行するか英語で聞いてください。

`research` の既定は API 自身の既定より意図的に絞ってあります（反復あたり 10 検索・2 反復・120 秒。API 既定は 20 / 4 / 180）。1 回の呼び出しで走った検索すべてとトークンが課金され、いったん投げた呼び出しは止められないためです。

### 設定

`~/.config/brave-search/config.toml` — 全キーは [config.example.toml](config.example.toml) を参照。優先順位は フラグ > 環境変数 > ファイル > 組み込み既定。未知のキーは拒否されます。組み込みの国・言語の既定は Brave 自身の既定（`US`・`en`）なので、日本語の結果が欲しければ `country = "JP"`・`search_lang = "ja"` を設定してください。

### MCP サーバ

`brave-search mcp` をクライアントに登録します。Claude Code なら:

```bash
claude mcp add brave-search -- /path/to/brave-search mcp
```

最初に `get_usage` を呼んでください。ツール・結果スキーマ・コストモデル・エラーコードの完全なリファレンスです。

## セットアップ

1. <https://api-dashboard.search.brave.com/> で契約し — **Search** プランが `web` と `context`、**Answers** プランが `answer` と `research` を担います — API キーを作成します。
2. Brave は**プランごとに別のキー**を発行します。Search のキーを `~/.config/brave-search/config.toml` の `api_key`（または `BRAVE_SEARCH_API_KEY`）に、Answers のキーを `answers_api_key`（または `BRAVE_SEARCH_ANSWERS_API_KEY`）に置きます — [config.example.toml](config.example.toml) 参照。キーはフラグでは受け付けません。
3. `brave-search auth check` で各キーの状態 — 有効・拒否・プラン未契約・未設定 — と、どの設定ファイルが読まれたかが分かります。プランごとに意図的に不正なリクエストを送って判定します。Brave は失敗応答を課金しないと文書化しているので、この確認は無料のはずです。

## 利用規約がこのツールに課すこと

Brave の [API 利用規約](https://api-dashboard.search.brave.com/documentation/resources/terms-of-service)は、検索結果の一時的な保持のみを許し、再配布と、AI モデルの学習・評価・改善への使用を禁じています。したがって:

- **キャッシュはありません。** 同じ呼び出しは 2 回目も課金されます。
- **すべての結果がコスト**と残りのレート枠を報告するので、支出が見えます。
- 結果は Brave とその出典元のものです。URL を引用してください。保存・再配布はせず、モデルの学習や評価にも使わないでください。推論時のモデル入力として使うことが、LLM Context と Answers エンドポイントの本来の用途です。

## 帰属

[公式 Brave Search skills](https://github.com/brave/brave-search-skills)（MIT）をエンドポイント仕様の参照元としました。コードは流用していません。

## ライセンス

MIT — [LICENSE](LICENSE) を参照。
