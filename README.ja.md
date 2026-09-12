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

> **Status: 開発中。** スキャフォールドはビルド・テストが通りますが、検索コマンドは未実装です。

## インストール

```bash
make build  # → dist/brave-search
```

## セットアップ

1. <https://api-dashboard.search.brave.com/> で契約し — **Search** プランが `web` と `context`、**Answers** プランが `answer` と `research` を担います — API キーを作成します。
2. キーを `~/.config/brave-search/config.toml`（[config.example.toml](config.example.toml) 参照）か `BRAVE_SEARCH_API_KEY` に置きます。キーはフラグでは受け付けません。
3. `brave-search auth check` で、そのキーがどのプランを使えるか確認できます。

## 利用規約がこのツールに課すこと

Brave の [API 利用規約](https://api-dashboard.search.brave.com/documentation/resources/terms-of-service)は、検索結果の一時的な保持のみを許し、再配布と、AI モデルの学習・評価・改善への使用を禁じています。したがって:

- **キャッシュはありません。** 同じ呼び出しは 2 回目も課金されます。
- **すべての結果がコスト**と残りのレート枠を報告するので、支出が見えます。
- 結果は Brave とその出典元のものです。URL を引用してください。保存・再配布はせず、モデルの学習や評価にも使わないでください。推論時のモデル入力として使うことが、LLM Context と Answers エンドポイントの本来の用途です。

## 帰属

[公式 Brave Search skills](https://github.com/brave/brave-search-skills)（MIT）をエンドポイント仕様の参照元としました。コードは流用していません。

## ライセンス

MIT — [LICENSE](LICENSE) を参照。
