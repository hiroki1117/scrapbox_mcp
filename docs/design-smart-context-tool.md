# 設計案: Smart Context（Export for AI）ツールの追加

API の仕様は [scrapbox-api.md](./scrapbox-api.md#smart-context-apiui名-export-for-ai) を参照。

## 目的

いまの MCP ツールでは、あるテーマについて LLM に文脈を渡すには `get_page` を何度も呼ぶ必要がある。
Smart Context を使えば、**起点ページと関連ページの本文を 1 リクエストで、関連度の高い順に** 取得できる。
Cosense 公式も「MCP で読む（1 リクエストで大量のページを取得できる）」という用途を想定している。

## 段階的な導入

| Phase | 内容 | 前提 |
|-------|------|------|
| 1 | `get_smart_context` ツール（1 hop / 2 hop・サイズ制御つき） | **実装済み** |
| 2 | 2 hop search による絞り込みパラメータの追加 | API パラメータの調査が必要 |
| 3 | signed URL 発行ツール（外部 AI に URL だけを渡す） | 2026 年の新機能。エンドポイントの調査が必要 |

---

## Phase 1: `get_smart_context`

### ツール名

`get_smart_context` を推奨する。

- 既存の読み取り系ツール（`get_page` / `list_pages` / `search_pages`）の命名に揃えられる
- API パス（`/api/smart-context`）と公式の機能名に一致する
- UI 名の「Export for AI」は description に書き、LLM がツールを選ぶときの手がかりにする

### 入力スキーマ

```json
{
  "type": "object",
  "properties": {
    "title":     { "type": "string",  "description": "起点ページのタイトル" },
    "hops":      { "type": "number", "enum": [1, 2], "default": 1,
                   "description": "1: 直接リンク/被リンクのページまで。2: 2 hop link まで含める（サイズがかなり大きくなる）" },
    "project":   { "type": "string",  "description": "プロジェクト名（省略時はデフォルト）" },
    "max_chars": { "type": "number", "default": 100000,
                   "description": "返す最大文字数。超えたときはページ単位で切り詰める" },
    "offset":    { "type": "number", "default": 0,
                   "description": "前回の続きを取得するときの開始位置（文字単位）" }
  },
  "required": ["title"]
}
```

### description（LLM 向け）の案

> Exports a Scrapbox page together with its related pages (linked, back-linked, and optionally 2-hop linked pages) as a single AI-ready text. Same as "Export for AI" in the Scrapbox page menu. Use this instead of calling get_page repeatedly when you need the surrounding context of a topic. Start with hops=1; use hops=2 only when more context is needed, because the output can exceed 1 MB.

### サイズ制御（いちばん重要）

2 hop は 1 MB を超えることがある（villagepump で 1,122 KB）。そのまま返すとクライアントのコンテキストを使い切ってしまう。

1. 全文を取得し、`offset` から `max_chars` 文字（rune 単位）を切り出す
2. 切り出す位置は `<Page` の開始位置に合わせ、ページが途中で切れないようにする（ページ境界が見つからないときは rune 境界で切る）
3. 切り詰めたときは、末尾に次のような注記を付ける

   ```
   [truncated: returned chars 0-99791 of 252747. Call again with offset=99791 to continue, or use hops=1.]
   ```

4. 先頭の LLM 向けガイドは `offset=0` のときだけ入る。続きを取得するときはガイドが入らないが、ページ単位で切っているので読むのに問題はない

`max_chars` のデフォルトは 100,000 文字、上限は 500,000 文字とする。1 hop ならほとんどの場合、1 回で全文を返せる。

> Smart Context はサーバー側で毎回全体を生成するので、`offset` を使うと同じ内容を何度もダウンロードすることになる。
> Phase 1 ではシンプルさを優先してキャッシュを持たない。問題が出たら `(project, title, hops)` をキーにした短い TTL（数分）のキャッシュを追加する。

### 変更するファイル

| ファイル | 変更内容 |
|----------|----------|
| `internal/scrapbox/rest.go` | `ExportSmartContext(project, title string, hops int) (string, error)` を追加 |
| `internal/tools/get_smart_context.go` | 新しいツール（引数の検証、切り詰め処理） |
| `cmd/server/main.go` | `registry.Register(tools.NewGetSmartContextTool(scrapboxClient))` |
| `CLAUDE.md` | Architecture と MCP Tools の表に追加 |
| `internal/tools/get_smart_context_test.go` | 切り詰め処理のユニットテスト（ページ境界、マルチバイト文字、offset） |

### REST クライアントの実装方針

```go
// ExportSmartContext retrieves the Smart Context (Export for AI) text for a page
func (c *RESTClient) ExportSmartContext(project, title string, hops int) (string, error) {
	endpoint := fmt.Sprintf("%s/smart-context/export-%dhop-links/%s.txt?title=%s",
		c.baseURL, hops, url.PathEscape(project), url.QueryEscape(title))
	// GetPage と同じ流れ: AddAuthHeaders → Do → ステータス確認 → io.ReadAll
	// JSON ではないので json.Unmarshal はせず、string(body) を返す
}
```

- `hops` は 1 または 2 だけを受け付ける（ツール側で検証する）
- エラーの対応
  - 400 → `ErrCodeInvalidInput`
  - 404 → `ErrCodeNotFound`（"Project not found"。空ページでも 200 が返るので、404 はプロジェクトがないときだけ）
  - 401 / 403 → 既存の `checkResponseStatus` のまま（`ErrCodeAuthFailed`）
- タイムアウト: 2 hop は生成に時間がかかる可能性がある。まずは既存の `REQUEST_TIMEOUT`（30 秒）で様子を見る

### 返り値

既存のツールは JSON を返しているが、このツールは **API が返すテキストをそのまま返す**。
Smart Context は最初から LLM に読ませるために整形されているので、JSON で包むとエスケープのせいでトークンが増え、読みにくくなるだけ。

### 検証手順

1. `go build ./...` / `go vet ./...` / 切り詰め処理のユニットテスト
2. `@test-server` エージェントで、実際のプロジェクトに対して 1 hop / 2 hop を呼ぶ
3. 実際のレスポンスでタグ構造（`<Page>` の閉じタグやメタ情報）を確認し、切り詰めの境界判定と `scrapbox-api.md` の TODO を更新する

---

## Phase 2: 2 hop search による絞り込み

- UI では関連ページリストを 2 hop search で絞り込み、その結果だけをエクスポートできる (#7530)
- API に何が渡されているか（クエリ文字列なのか、ページ ID のリストを POST しているのか）はまだわかっていない
- わかったら `get_smart_context` に `filter`（文字列）引数を追加する。2 hop の出力サイズを抑えるいちばん効果的な方法になる
- 調査: `@scrapbox-spec` エージェント、またはブラウザの DevTools で UI 操作時のリクエストを確認する

## Phase 3: signed URL の発行

- 2026 年に「Private Project の Smart Context を外部 AI に渡すための signed URL 発行機能」が追加された (#8154)。発行回数に制限がある (#8167)
- MCP のツールにすると、**本文をクライアントのコンテキストに流さずに、URL だけを外部 AI（NotebookLM、Gemini、別のエージェントなど）に渡せる**
- ツール案: `create_smart_context_url`（入力: `title`, `hops`, `project` / 出力: URL と有効期限）
- 注意点
  - URL を持っている人は誰でも private project の内容を読めるので、description に有効期限と取り扱いの注意を明記する
  - 発行回数に制限があるので、429 を `ErrCodeRateLimit` に変換する
- エンドポイント・パラメータ・有効期限を調査してから実装する

## 採用しなかった案

- **`get_page` に `with_related` オプションを追加する**: 返り値の形式（JSON とテキスト）もサイズの性質もまったく違うので、ツールを分けたほうが LLM が使い分けやすい
- **1 hop と 2 hop を別々のツールにする**: 同じことをするツールが増えるだけ。`hops` 引数で十分
- **MCP Resources（`scrapbox://…/smart-context/…`）として公開する**: 現在のサーバーは Tools しか実装しておらず、Resources を使えるクライアントも限られるので、今回は見送る
