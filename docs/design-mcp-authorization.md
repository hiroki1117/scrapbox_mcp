# MCP 認証（Authorization）仕様の整理と Scrapbox MCP への実装検討

調査日: 2026-09-27
対象仕様: MCP **2026-07-28**（現行 latest）の Authorization 章と、Claude コネクタ側の認証要件

> このドキュメントは設計検討です。まだ実装していません。

---

## 1. 現状の整理（このリポジトリ）

| 項目 | 現状 |
|------|------|
| MCP クライアント → 本サーバー | **認証なし**。`/mcp` に届いたリクエストはすべて処理する |
| 本サーバー → Scrapbox | 環境変数 `COSENSE_SID`（オーナーの `connect.sid` Cookie）を固定で付与（`internal/scrapbox/auth.go`） |
| 防御 | `Origin` ヘッダ検証のみ（`ALLOWED_ORIGINS` 未設定なら全許可）。`Origin` がないリクエストは素通し |
| プロトコルバージョン | `2024-11-05` 固定（`internal/mcp/handler.go`） |
| デプロイ | Cloud Run（公開 URL） |

**問題点**: Cloud Run の URL を知っていれば誰でも、オーナー権限で Scrapbox プロジェクトの読み書き
（`edit_page` でページ全置換も含む）ができる。`Origin` 検証はブラウザからの DNS rebinding 対策で、
curl などブラウザ以外のクライアントは `Origin` を送らないため防御になっていない。

---

## 2. MCP 認証仕様の整理（2026-07-28）

### 2.1 位置づけ

- 認証は **OPTIONAL**。ただし HTTP 系トランスポートで認証をするなら本仕様に **SHOULD** 準拠。
  STDIO は環境変数などで資格情報を渡し、本仕様には従わない（SHOULD NOT）。
- ベースは **OAuth 2.1（draft-ietf-oauth-v2-1-13）** のサブセット。

### 2.2 登場人物

| 役割 | MCP での対応 |
|------|--------------|
| Resource Server (RS) | **MCP サーバー**（本リポジトリ） |
| Client | MCP クライアント（Claude.ai / Claude Code など） |
| Authorization Server (AS) | トークン発行者。MCP サーバーと同居でも外部 IdP でもよい |

### 2.3 関連 RFC と MUST/SHOULD の対応

| 仕様 | 内容 | MCP での扱い |
|------|------|-------------|
| OAuth 2.1 | 認可コード + PKCE | AS は MUST 実装 |
| RFC 9728 Protected Resource Metadata (PRM) | RS が「自分の AS はここ」を公開 | **MCP サーバーは MUST 実装** |
| RFC 8414 AS Metadata / OIDC Discovery | AS のエンドポイント公開 | AS はどちらか MUST、クライアントは両対応 MUST |
| RFC 8707 Resource Indicators | `resource` パラメータでトークンの宛先を指定 | クライアント MUST 送信、サーバーは audience 検証 MUST |
| RFC 6750 Bearer | `Authorization: Bearer` | 全 HTTP リクエストに付与 MUST、クエリ文字列禁止 |
| RFC 9207 Issuer Identification | 認可レスポンスの `iss` | AS は SHOULD 付与（将来 MUST 予定）、クライアントは検証 MUST |
| Client ID Metadata Documents (CIMD) | HTTPS URL を `client_id` にする | **SHOULD（推奨の登録方式）** |
| RFC 7591 Dynamic Client Registration (DCR) | `POST /register` | MAY、**2026-07-28 で deprecated**（後方互換のため残存） |

### 2.4 フロー

```
Client ──(トークンなし)──▶ MCP Server
       ◀── 401 WWW-Authenticate: Bearer resource_metadata="…", scope="…"
Client ──GET PRM──▶ MCP Server  →  { resource, authorization_servers:[AS], scopes_supported }
Client ──GET AS metadata──▶ AS   （/.well-known/oauth-authorization-server → openid-configuration の順に試行）
       クライアント登録: 事前登録 > CIMD > DCR > ユーザー入力 の優先順
Client ──ブラウザで /authorize（PKCE S256, resource, scope, state）──▶ AS
       ◀── redirect ?code=…&state=…&iss=…   （クライアントは iss を検証）
Client ──POST /token（code_verifier, resource）──▶ AS  → access_token (+ refresh_token)
Client ──Authorization: Bearer …──▶ MCP Server
```

### 2.5 MCP サーバー（RS）側の要件まとめ

**ディスカバリ**
- PRM を提供（MUST）。`authorization_servers` に最低 1 つ（MUST）。
- 次のいずれかで PRM の場所を示す（MUST）:
  1. 401 の `WWW-Authenticate` に `resource_metadata="…"`
  2. well-known: エンドポイントパス付き `/.well-known/oauth-protected-resource/mcp`、またはルート `/.well-known/oauth-protected-resource`
- 401 の `WWW-Authenticate` に `scope="…"` を含めるのが SHOULD（最小権限の誘導）。
- PRM の `scopes_supported` は「基本機能に必要な最小スコープ」を表す。追加権限は step-up で要求する。
- `offline_access` は `WWW-Authenticate` にも `scopes_supported` にも入れない（SHOULD NOT）。

**トークン検証**
- OAuth 2.1 §5.2 に従い検証（MUST）。**自分宛て（audience）であること**を検証（MUST）。
- 無効・期限切れは **401**（MUST）。
- 他のリソース向けトークンを受け付けない・中継しない（MUST NOT）。
  → **Token passthrough 禁止**: 受け取ったトークンを上流 API にそのまま渡してはいけない。

**エラー**

| Status | 用途 |
|--------|------|
| 401 | 認証が必要 / トークン無効 |
| 403 | スコープ不足。`WWW-Authenticate: Bearer error="insufficient_scope", scope="…", resource_metadata="…"` |
| 400 | 不正な認可リクエスト |

- スコープ不足時は、その操作に必要なスコープを **1 回のチャレンジでまとめて**返す（SHOULD）。
- スコープの階層（広いスコープが狭いスコープを含む）を考慮して判定（MUST）。
- スコープの蓄積（以前のスコープとの和集合）はクライアントの責任。

**Canonical URI**
- `resource` はサーバーの正規 URI（例: `https://mcp.example.com/mcp`）。スキーム必須、フラグメント禁止、末尾スラッシュなしを推奨。

### 2.6 AS 側の要件（自前で AS を持つ場合に関係）

- OAuth 2.1、PKCE `S256`。メタデータに `code_challenge_methods_supported` がないとクライアントは中断する（MUST）。
- 全エンドポイント HTTPS。redirect URI は `localhost` か HTTPS のみ。**完全一致**で検証（MUST）。
- `iss` を認可レスポンスに付与するなら `authorization_response_iss_parameter_supported: true` を広告（MUST）。
- パブリッククライアントの refresh token は **ローテーション** MUST。アクセストークンは短命が SHOULD。
- CIMD 対応時: `client_id` URL を fetch、`client_id` 一致・`redirect_uris` 照合（MUST）、SSRF 対策（SHOULD）、
  同意画面に redirect URI のホスト名を明示（MUST）、localhost のみの場合は警告（SHOULD）。
- **Confused deputy**: 上流の第三者 AS に対して静的 client ID を使うプロキシ型サーバーは、
  動的登録されたクライアントごとにユーザー同意を取ってから上流に転送（MUST）。

### 2.7 2026-07-28 で変わった点（認証まわり）

- **RFC 9207 `iss` 検証**の追加（mix-up 攻撃対策）。
- **DCR が deprecated**、CIMD が標準に。DCR 時は `application_type`（`native` / `web`）指定 MUST。
- クライアント資格情報を **発行元 AS（issuer）にバインド**（AS 変更時は再登録）。
- **ステートレス化**: `initialize` ハンドシェイク廃止、`Mcp-Session-Id` deprecated。
  各リクエストが `_meta` にプロトコルバージョン等を持ち、`Mcp-Method` / `Mcp-Name` ヘッダが付く。
  → 認証をセッションに紐づける設計は不可。**リクエスト単位で毎回トークン検証**するのが前提。
- 認証拡張（ext-auth リポジトリ）: **Enterprise-Managed Authorization（stable、ID-JAG による SSO）**、
  **Client Credentials（draft、M2M）**。いずれも任意。

### 2.8 Claude（クライアント）側の実装上の注意

Claude.ai / Desktop / mobile / Claude Code は同じ認証基盤。仕様より厳しい / 具体的な点:

- サインイン開始には **401 が必須**（200 に `WWW-Authenticate` を付けても無視）。
- PRM の `resource` は **ユーザーが入力した URL と完全一致**（パス含む）。
- `authorization_servers` は **先頭 1 件のみ**使用。
- CIMD を使うのは AS メタデータに `client_id_metadata_document_supported: true` **かつ**
  `token_endpoint_auth_methods_supported` に `"none"` がある場合のみ。なければ DCR にフォールバック。
- redirect URI: `https://claude.ai/api/mcp/auth_callback`（将来 `https://claude.com/api/mcp/auth_callback` も）と、
  Claude Code 用ループバック `http://localhost:<任意port>/callback` / `http://127.0.0.1:<任意port>/callback`（**ポート無視で照合**）。
- `/token` は `application/x-www-form-urlencoded` を受理すること。refresh 失敗時は `invalid_grant` を返す。
- 期限の 5 分前にプロアクティブ refresh、401 でリアクティブ refresh。
- タイムアウト: discovery / register / token は 10 秒、refresh は 30 秒。
- `client_credentials`（M2M）は非対応。
- 静的ヘッダ認証（API キー）は **beta・一部 Organization のみ**（Claude Code の `.mcp.json` では `headers` 指定が可能）。
- 送信元 IP: `160.79.104.0/21`。

---

## 3. Scrapbox MCP 固有の論点

1. **上流（Scrapbox）に OAuth がない。** Scrapbox の認証は `connect.sid` Cookie のみ。
   よって「MCP のトークンを上流 IdP のトークンと交換する」構成はとれない。
   上流資格情報は常にサーバー側で保持し、MCP トークンとは完全に分離する（Token passthrough 禁止にも自然に合致）。
2. **利用者は実質オーナー 1 人（シングルテナント）。** `COSENSE_SID` は 1 つで、全ツールがオーナー権限で動く。
   認証の目的は「オーナー（と許可した人）以外を締め出す」こと。
3. **書き込み系ツールの影響が大きい。** `edit_page` は全文置換。読み取りと書き込みでスコープを分ける価値がある。
4. **Cloud Run（複数インスタンス・スケールゼロ）。** インメモリ状態は共有されない。
   アクセストークンは自己検証可能な JWT にし、状態が必要なもの（認可コード、refresh token）は
   短命 + 署名/暗号化でステートレスにするか、Firestore 等に置く。

---

## 4. 実装方針の選択肢

| 案 | 概要 | 仕様準拠 | Claude.ai から使える | 実装コスト | 備考 |
|----|------|----------|----------------------|-----------|------|
| **A. 静的 Bearer トークン** | `MCP_API_KEY` を照合するだけ | OAuth ではない（認証自体は任意なので違反ではない） | beta の static headers 対象 Org のみ。Claude Code は `headers` で可 | 極小 | 暫定対策として即効性あり |
| **B. 外部 AS（Auth0 / WorkOS AuthKit / Keycloak 等）** | MCP サーバーは RS のみ（PRM + JWT 検証） | ◎ | ◎（AS が CIMD/DCR 対応なら） | 小〜中 | 外部 SaaS 依存。AS 側で RFC 8707 audience・CIMD 対応が必要 |
| **C. 内蔵 AS（Go で自前実装）+ Google ログイン** | 本サーバーが AS も兼ねる。ユーザー認証は Google OIDC に委譲しメール許可リストで判定 | ◎（自前で満たす） | ◎ | 中〜大 | 外部依存なし（Google のみ）。セキュリティ実装の責任が自分に来る |
| D. マルチテナント化 | 同意画面で各ユーザーが自分の `connect.sid` を登録し、`sub` に紐づけ暗号化保存 | ◎ | ◎ | 大 | 現状の要件（個人用）には過剰。将来案 |

補足: Google を直接 AS として `authorization_servers` に書く案は不可。
Google は RFC 8707 の `resource` による audience 指定・CIMD・DCR に対応しておらず、
MCP クライアント（Claude）が Google に client 登録できないため。

### 推奨

- **Phase 0（すぐ）: 案 A** — 現在オープンな書き込み権限を塞ぐ。数十行で済む。
- **Phase 1: 案 C（内蔵 AS + Google ログイン + CIMD/DCR）**、外部 SaaS を許容するなら **案 B** の方が安全で速い。
  個人運用・Cloud Run・Go 単体バイナリという現状の構成を崩さない点で C を第一候補とし、
  以下はその設計。B を選ぶ場合は §5.1〜5.3（RS 側）だけ実装すればよい。

---

## 5. 設計（案 C ベース。RS 部分は案 B と共通）

### 5.1 エンドポイント

| Path | 役割 | 認証 |
|------|------|------|
| `POST/GET/DELETE /mcp` | MCP 本体 | **Bearer 必須** |
| `GET /.well-known/oauth-protected-resource/mcp` | PRM（パス付き） | 公開 |
| `GET /.well-known/oauth-protected-resource` | PRM（ルート、フォールバック） | 公開 |
| `GET /.well-known/oauth-authorization-server` | AS メタデータ（RFC 8414） | 公開 |
| `GET /authorize` | 認可エンドポイント → Google ログインへリダイレクト | 公開 |
| `GET /oauth/google/callback` | Google OIDC コールバック → 同意画面 | 公開 |
| `POST /authorize/consent` | 同意確定 → クライアントへ `code` + `state` + `iss` を返す | CSRF トークン |
| `POST /token` | `authorization_code` / `refresh_token` グラント（form-urlencoded） | PKCE / client 認証 |
| `POST /register` | DCR（deprecated だが Claude のフォールバック用に用意） | 公開（レート制限） |
| `GET /jwks.json` | アクセストークン検証用公開鍵（外部検証用、任意） | 公開 |
| `GET /health` | ヘルスチェック | 公開のまま |

### 5.2 メタデータ例

PRM（`MCP_RESOURCE_URL=https://scrapbox-mcp-xxxx.a.run.app/mcp` の場合）:

```json
{
  "resource": "https://scrapbox-mcp-xxxx.a.run.app/mcp",
  "authorization_servers": ["https://scrapbox-mcp-xxxx.a.run.app"],
  "scopes_supported": ["scrapbox:read"],
  "bearer_methods_supported": ["header"],
  "resource_name": "Scrapbox MCP (<project>)"
}
```

`scopes_supported` は仕様どおり「基本機能の最小セット」= 読み取りのみとし、書き込みは step-up で要求する。
（ただし毎回の step-up が煩わしければ `scrapbox:read scrapbox:write` を最初から要求する運用も可。§5.4 参照）

AS メタデータ:

```json
{
  "issuer": "https://scrapbox-mcp-xxxx.a.run.app",
  "authorization_endpoint": "https://scrapbox-mcp-xxxx.a.run.app/authorize",
  "token_endpoint": "https://scrapbox-mcp-xxxx.a.run.app/token",
  "registration_endpoint": "https://scrapbox-mcp-xxxx.a.run.app/register",
  "jwks_uri": "https://scrapbox-mcp-xxxx.a.run.app/jwks.json",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code", "refresh_token"],
  "code_challenge_methods_supported": ["S256"],
  "token_endpoint_auth_methods_supported": ["none"],
  "client_id_metadata_document_supported": true,
  "authorization_response_iss_parameter_supported": true,
  "scopes_supported": ["scrapbox:read", "scrapbox:write", "offline_access"]
}
```

- `token_endpoint_auth_methods_supported` に `"none"` と `client_id_metadata_document_supported: true` の両方が
  ないと Claude は CIMD を使わない点に注意。
- `offline_access` は **AS 側**の `scopes_supported` にだけ載せる（Claude が refresh token を要求するため）。PRM には載せない。

### 5.3 RS 側: 認証ミドルウェア（`internal/auth`）

```
internal/auth/
├── middleware.go   // Bearer 抽出・検証・401/403 の WWW-Authenticate 生成
├── metadata.go     // PRM ハンドラ
├── token.go        // JWT 検証（iss / aud / exp / nbf / scope）
└── scope.go        // ツール → 必要スコープ、階層判定
```

処理:

1. `Authorization: Bearer <token>` を取り出す。なし / 不正形式 → **401**
   ```
   WWW-Authenticate: Bearer resource_metadata="https://…/.well-known/oauth-protected-resource/mcp", scope="scrapbox:read"
   ```
2. JWT 検証: 署名（ES256）、`iss == AS issuer`、**`aud` に `MCP_RESOURCE_URL` を含む**、`exp`/`nbf`、`sub` が許可リスト内。
   失敗 → **401**（`error="invalid_token"`）。クエリ文字列のトークンは読まない。
3. 検証済みクレーム（`sub`, `email`, `scope`）を `context.Context` に入れて次へ。
4. **ツール単位のスコープ判定**は JSON-RPC をパースした後で行う必要があるため `Transport.HandlePOST` 内で実施
   （2026-07-28 以降のクライアントなら `Mcp-Method` / `Mcp-Name` ヘッダでも判定可能）:
   - `tools/call` で `name` が書き込み系 かつ `scrapbox:write` なし → **HTTP 403**
     ```
     WWW-Authenticate: Bearer error="insufficient_scope", scope="scrapbox:read scrapbox:write",
                       resource_metadata="https://…/.well-known/oauth-protected-resource/mcp",
                       error_description="scrapbox:write is required for edit_page"
     ```
   - スコープ階層: `scrapbox:write` は `scrapbox:read` を包含する扱いにする（MUST 要件）。
5. **全メソッド（POST/GET/DELETE）に適用**。`OPTIONS`（CORS プリフライト）と `/health`、well-known は除外。

スコープ定義（`tools.Tool` インターフェースに `RequiredScope() string` を追加して registry から引く）:

| スコープ | ツール |
|----------|--------|
| `scrapbox:read` | `get_page`, `list_pages`, `search_pages`, `get_smart_context` |
| `scrapbox:write`（read を包含） | `insert_lines`, `create_page`, `edit_page` |

`tools/list` はスコープで絞らず全件返す（クライアントが step-up を判断できるように）。

### 5.4 AS 側（`internal/oauth`）

```
internal/oauth/
├── server.go        // ハンドラ群のルーティング、AS メタデータ
├── authorize.go     // /authorize のパラメータ検証、Google へのリダイレクト
├── consent.go       // 同意画面（クライアント名・redirect ホスト名・要求スコープを表示）
├── token.go         // /token（code 交換・refresh ローテーション）
├── register.go      // DCR（deprecated、フォールバック用）
├── cimd.go          // Client ID Metadata Document の取得・検証（SSRF 対策）
├── keys.go          // 署名鍵（Secret Manager から ES256 秘密鍵をロード）、JWKS
└── store.go         // refresh token / 使用済み code の保存（Firestore or インメモリ）
```

**/authorize の検証**
- `response_type=code`、`code_challenge` + `code_challenge_method=S256` 必須。
- `resource` が `MCP_RESOURCE_URL` と一致すること（大文字スキーム/ホストは許容）。
- `client_id`:
  - `https://` かつパス付き URL → **CIMD**: fetch（タイムアウト数秒・サイズ上限・プライベート IP 拒否・リダイレクト不追従）、
    `client_id` 一致、`redirect_uri` が `redirect_uris` に含まれるか確認。キャッシュは HTTP ヘッダ準拠。
  - それ以外 → DCR 登録済みクライアントとして照合。
- `redirect_uri` の照合は完全一致。ただし `http://localhost` / `http://127.0.0.1` は **ポート無視**で照合（Claude Code 対応）。
- 追加の許可リスト（任意）: `ALLOWED_REDIRECT_HOSTS`（例: `claude.ai, claude.com, localhost, 127.0.0.1`）で信頼しない
  クライアントを弾く。個人用サーバーなので厳しめでよい。
- `state`・PKCE・`client_id`・`redirect_uri`・`resource`・`scope` を **署名付き短命 Cookie** に保持して Google へ
  （Cloud Run の複数インスタンス対策として、サーバー状態を持たない）。

**ユーザー認証（Google OIDC）**
- `golang.org/x/oauth2` + ID トークン検証。`email_verified == true` かつ `AUTH_ALLOWED_EMAILS` に含まれるもののみ許可。
- Google から得たトークンは **ユーザー識別にだけ使い、保持も上流転送もしない**。

**同意画面（confused deputy 対策、MUST 相当）**
- 本サーバーは Google に対して静的 client ID を使う「プロキシ型」になるため、**MCP クライアントごとに同意を取る**。
- 表示: クライアント名（CIMD の `client_name`）、**redirect URI のホスト名**、要求スコープ、対象 Scrapbox プロジェクト名。
  localhost のみのクライアントには警告を出す。
- 同意済み `(sub, client_id)` を記録すれば 2 回目以降は省略可能（ただし書き込みスコープの新規付与時は再同意）。

**認可コード**
- 60 秒程度の短命。ワンタイム性が必要なので、ステートレスにするなら「暗号化した code + 使用済み JTI を Firestore / インスタンス内 LRU で記録」。
  Cloud Run で `max-instances=1` にするならインメモリでも成立する（個人用途なら現実的な割り切り）。
- レスポンスに `iss` を付与（RFC 9207）。

**トークン**
- アクセストークン: ES256 JWT、有効期限 15〜60 分。
  クレーム: `iss`, `sub`, `aud`（= `MCP_RESOURCE_URL`）, `scope`, `client_id`, `exp`, `iat`, `jti`。
- refresh token: パブリッククライアントなので **ローテーション必須**。
  不透明トークン（ランダム 256bit）を Firestore にハッシュで保存し、使用時に失効 + 新規発行。
  再利用を検知したら同じファミリーを全失効。
  （永続ストアを持ちたくない場合は refresh token を発行しない選択肢もあるが、1 時間ごとに再ログインになる）
- `/token` は form-urlencoded、エラーは RFC 6749 準拠（refresh 失敗は `invalid_grant`）。応答は 10 秒以内。

### 5.5 既存コードへの変更点

| ファイル | 変更 |
|---------|------|
| `internal/config/config.go` | `AUTH_MODE`(`none`/`static`/`oauth`)、`MCP_API_KEY`、`MCP_RESOURCE_URL`、`OAUTH_ISSUER`、`AUTH_ALLOWED_EMAILS`、`GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET`、`OAUTH_SIGNING_KEY`、`ACCESS_TOKEN_TTL`、`ALLOWED_REDIRECT_HOSTS` を追加 |
| `cmd/server/main.go` | `/mcp` を認証ミドルウェアでラップ。well-known / OAuth エンドポイントを登録。`AUTH_MODE=none` の起動時に警告ログ |
| `internal/mcp/transport.go` | `tools/call` のスコープ判定と 403 応答。CORS の `Access-Control-Allow-Headers` に `Authorization`、`Expose-Headers` に `WWW-Authenticate` を追加。`HandleGET` のコメント（OAuth 誤検知回避のための 405）は、認証有効時はミドルウェアが先に 401 を返すので見直し |
| `internal/tools/registry.go` | `Tool` に `RequiredScope()` を追加（または registry に書き込みツールの集合を持たせる） |
| `internal/scrapbox/*` | **変更なし**。`COSENSE_SID` はサーバー側で保持し続ける（MCP トークンとは無関係） |

実装上の注意:
- Cloud Run の背後では `r.Host` / スキームが信頼できないため、URL は必ず `MCP_RESOURCE_URL` / `OAUTH_ISSUER` 設定値から組み立てる。
- トークン・Cookie・`COSENSE_SID` はログに出さない。
- 秘密鍵・Google client secret・`COSENSE_SID` は Secret Manager 経由で注入。
- `Origin` 検証は DNS rebinding 対策として残す（認証とは別レイヤー）。
- 定数時間比較（`crypto/subtle`）を API キー照合に使う。

### 5.6 Phase 0（静的 API キー）の最小実装イメージ

```go
// internal/auth/static.go
func StaticBearer(apiKey, resourceMetadataURL string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Claude Code での利用:

```json
{
  "mcpServers": {
    "scrapbox": {
      "type": "http",
      "url": "https://scrapbox-mcp-xxxx.a.run.app/mcp",
      "headers": { "Authorization": "Bearer ${SCRAPBOX_MCP_API_KEY}" }
    }
  }
}
```

注意: Phase 0 を有効にすると、static headers（beta）が使えない Claude.ai のカスタムコネクタからは接続できなくなる。
Claude.ai から使い続けたいなら Phase 1（OAuth）が必要。

### 5.7 テスト方針

- `internal/auth`: 401/403 の `WWW-Authenticate` 形式、`aud` 不一致・期限切れ・署名不正の拒否、スコープ階層。
- `internal/oauth`: PKCE 検証、redirect URI 照合（ループバックのポート無視含む）、CIMD の `client_id` 不一致拒否・SSRF 拒否、
  refresh ローテーションと再利用検知、`iss` 付与。
- E2E: MCP Inspector / Claude Code / Claude.ai カスタムコネクタで接続確認（`test-server` エージェント）。

### 5.8 別途検討事項

- プロトコルバージョンが `2024-11-05` のまま。認証はトランスポート層なので独立して導入できるが、
  2026-07-28 のステートレス化（`initialize` 廃止、`Mcp-Session-Id` deprecated）への追従は別タスクで検討する。
  セッションを残す間は、セッション ID をトークンの `sub` に紐づけ、別ユーザーのトークンでの使い回しを拒否する。
- Enterprise-Managed Authorization（ID-JAG）は組織 SSO 向けで、個人運用の本サーバーでは不要。

---

## 参考

- [MCP Authorization (2026-07-28)](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)
- [Authorization Server Discovery](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/authorization-server-discovery)
- [Client Registration](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration)
- [Authorization Security Considerations](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/security-considerations)
- [The 2026-07-28 Specification（MCP Blog）](https://blog.modelcontextprotocol.io/posts/2026-07-28/)
- [MCP Authorization Extensions (ext-auth)](https://github.com/modelcontextprotocol/ext-auth)
- [Authentication for connectors（Claude Docs）](https://claude.com/docs/connectors/building/authentication)
