# Scrapbox REST API 仕様メモ

## Smart Context API（UI名: Export for AI）

調査日: 2026-09-26

### 概要

- ページを起点に、そのページと [関連ページリスト] 上のページ本文を **1つのプレーンテキスト** にまとめて返す API
- UI では Page Menu（Page Edit Menu）の `Export for AI (1 hop links)` / `Export for AI (2 hop links)` から利用できる
- 2025-04-10 頃にリリースされ、後に機能名が「Smart Context」に変わり、API が `/api/smart-context` に移動した (#7341)
- 用途: NotebookLM / Gemini / Claude Code などの LLM に「関連する文脈だけ」をまとめて渡すこと。MCP から大量のページを 1 リクエストで取得する用途も公式に想定されている

### エンドポイント

```
GET https://scrapbox.io/api/smart-context/export-1hop-links/:project.txt?title=:pageTitle
GET https://scrapbox.io/api/smart-context/export-2hop-links/:project.txt?title=:pageTitle
```

| 項目 | 内容 |
|------|------|
| `:project` | プロジェクト名（パスの末尾に `.txt` が付く） |
| `title` | 起点ページのタイトル（クエリパラメータ）。空ページ（リンクだけ存在するページ）も起点にできる (#7429) |
| `pageId` | 旧仕様のパラメータ。#7429 で `title` 指定に変更されたため **使わない** |
| 認証 | `connect.sid` Cookie。Business プランでは `x-service-account-access-key` ヘッダ（Service Account）も使える |
| レスポンス | `200 text/plain`（AI 向けに整形されたテキスト） |

### 範囲

- **1 hop**: 起点ページ + 直接リンク / 被リンクのページ（1 hop link）
- **2 hop**: 1 hop に加えて、2 hop link（リンク先が共通しているページ）も含む
- ページは「Related 順」で並ぶ (#7409)

### レスポンスの形式

- 先頭に LLM 向けのガイド文が入る (#7536)。内容は次のとおり
  - このファイルには複数のページが入っていて、各ページが URL を持ち、互いにリンクしている
  - `<Page>` を順に読むこと
  - 記述が矛盾するときは各 `<Page>` の更新日時を参考にすること
- 本文は `<PageList>` / `<Page>` のような疑似 XML タグで区切られる
- Scrapbox 記法（ブラケティング）はそのまま残る

> TODO: 実際のレスポンスを取得して、タグ構造（閉じタグの有無、各 Page に入るメタ情報のフィールド名）を確認する。
> 現時点の情報は shokai / villagepump のページと @cosense/std の実装から得たもの。

### サイズの目安

villagepump（日記型の巨大プロジェクト）で実際に計測された値:

- 1 hop: 約 66 KB
- 2 hop: 約 1,122 KB

2 hop は 1 MB を超えることがあるため、MCP のレスポンスにそのまま返すとコンテキストを圧迫する。

### エラー（@cosense/std の型定義より）

| Status | 意味 |
|--------|------|
| 400 | BadRequestError（title が空など） |
| 401 | NotLoggedInError |
| 403 | NotMemberError（private project のメンバーではない） |
| 404 | NotFoundError（プロジェクトが存在しない） |

### アクセス権

- 自分がメンバーになっている project はエクスポートできる
- ログインしているユーザーであれば、全ての Public Project のエクスポートもできる (#7343)
- Service Account（Business プラン）でも Smart Context をダウンロードできる

### 関連する機能（仕様がまだわかっていないもの）

- **2 hop search で絞り込んでからエクスポートする機能** (#7530)
  - UI 上で関連ページリストを検索して絞り込み、その結果だけをエクスポートする
  - どのパラメータで絞り込みが API に渡されるかは未調査
- **Private Project の Smart Context を外部 AI に渡すための signed URL 発行機能** (#8154, 2026)
  - 有効期限付きの URL を発行し、ログインしていない外部 AI からも取得できるようにするものと思われる
  - `RateLimitForSmartContextSignedUrlDocument` というモデルがあり、発行回数に制限がある (#8167)
  - エンドポイントとパラメータは未調査
- 2026-09-25 に help-jp で `Export for AI` と `AIエージェント` のページが新しく作られた（調査時点では本文なし）。公式ドキュメントが整備されている途中と考えられるので、再確認すること

### 参考

- [help-jp: Smart Context](https://scrapbox.io/help-jp/Smart_Context)
- [help-jp: Service Account](https://scrapbox.io/help-jp/Service_Account)
- [help-jp: リリースノート2025](https://scrapbox.io/help-jp/リリースノート2025) / [リリースノート2026](https://scrapbox.io/help-jp/リリースノート2026)
- [villagepump: Export for AI](https://scrapbox.io/villagepump/Export_for_AI)
- [mtane0412: CosenseのSmart Context API](https://scrapbox.io/mtane0412/CosenseのSmart_Context_API)
- [shokai: Smart Contextの先頭にLLM向けガイドを追加した](https://scrapbox.io/shokai/Smart_Contextの先頭にLLM向けガイドを追加した)
- [@cosense/std `api/smart-context`](https://jsr.io/@cosense/std/doc)（`export1HopLinks` / `export2HopLinks`、UNSTABLE 扱い）
