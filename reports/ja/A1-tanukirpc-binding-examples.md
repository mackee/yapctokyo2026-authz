# Transformer + 認可オブジェクト + 型で要求するモデル関数で縛れそうな例（5件）

haiku-attempt のトライアル（001〜015）で、Haiku が実際に間違えた箇所と、採点役が潜在リスクとして挙げた箇所から選んだ。
tanukirpc で実装はしていない。case-3 と同じ語彙を使ったスケッチである。

- **Transformer**: ルートごとに、認可済みのオブジェクトだけを Registry に載せる。生の store は Transformer のクロージャの中だけに置く。
- **認可オブジェクト（capability）**: `AcquireXxx` だけが作れる、非公開の具象型。
- **モデル関数**: 認可オブジェクトを引数の型で要求する。状態を変える唯一の入口になる。

共通の前提として、ハンドラから `*Store` に届かないことが要る（case-3 の「raw store unreachable」）。
これが崩れると、以下の型の縛りはすべて「迂回できる約束」に戻る。

---

## 1. 移動先での担当者ルール（観測: 移動トライアル 6本中6本で欠落）

**観測**
- 004・005・009・010・011・012 のすべてで、`moveTask` は移動先の 404/403/409 は確認した。
- しかし「担当者は移動先の contributor/manager でなければ 400」を確認せず、`task.ProjectID = target.ID` で終わっていた。
- 例: ivy が ben の t1 を web から ops に移すと、ops の非メンバーが担当者のまま残る。
- 004 では Haiku 自身が「再チェックしない」と README に書き、005 では自分のテストで 200 を期待していた。
- 対照として、複製（008）と PATCH での付け替え（013）は担当者を検証できていた。複製は `createTask` を、付け替えは既存 PATCH の担当者検証を写したからである。

**縛り方**
```go
// 移動先の Transformer が作る。書き込み権限（contributor/manager）、同一 org、未アーカイブを確認済み。
type WriteProject interface {
    ID() string
    // 担当者は「このプロジェクトの書き手」であることを確認したうえでしか作れない。
    AdmitAssignee(userID string) (AssigneeIn, error)
}
type AssigneeIn struct{ project, user string } // フィールドは非公開。WriteProject 以外では作れない

// プロジェクトを変える唯一のモデル関数。担当者の再判定を型で要求する。
func MoveTask(src EditTask, dst WriteProject, assignee AssigneeIn) error // 内部で assignee.project == dst.ID() も確認
```
- `task.ProjectID` を直接書けないので、移動は `MoveTask` を通るしかない。
- `MoveTask` は `AssigneeIn` を要求するので、`dst.AdmitAssignee(task.Assignee)` を呼ばずにはコンパイルできない。
- 「作成側にだけある制約」を、移動先の認可オブジェクトに持たせる形にしている。複製・移動・付け替え・統合（014）が、同じ `AdmitAssignee` を通ることになる。

**限界**: 移動先が同じ `AssigneeIn` を、別のタスクに使い回す誤りまでは型だけでは防げない（実行時チェックで補う）。

---

## 2. ロール降格と「担当者は書き手」不変条件（観測: 015 で ATOM 6件 FAIL）

**観測**
- 015 で Haiku は、`store.go` に `func (s *Store) SetRole(...) { s.members[p][u] = role }` を追加した。
- ハンドラは、manager かどうか・アーカイブ状態などを正しく確認したあとで、`SetRole` を直接呼んでいた。
- その結果、t1 の担当者 ben を web で viewer に降格すると、ben が「viewer なのに担当者」として残った（PUT は 200）。
- 呼び出し元の確認はすべて正しかった。壊れたのは、ほかのエンティティにまたがる不変条件である。

**縛り方**
```go
// project の manager だけが Transformer から受け取れる。アーカイブ済みなら取得の時点で 409。
type ManageMembers interface {
    Project() string
    // ロール変更の唯一の入口。viewer への降格では、担当タスクの扱いを呼び出し側に選ばせる。
    ChangeRole(user string, role Role, onAssigned AssignedTaskPolicy) (RoleChange, error)
}
type AssignedTaskPolicy interface{ isPolicy() }                // 非公開マーカー
var RejectIfAssigned, UnassignTasks AssignedTaskPolicy        // 2つだけ用意する
```
- store の `SetRole` 相当は capability パッケージの中に閉じ込め、ハンドラからは呼べないようにする。
- `onAssigned` が必須引数なので、ハンドラを書く側（エージェント）は不変条件の存在に型で気づかされる。
- 実際の確認は `ChangeRole` の中で 1回だけ実装すればよく、ハンドラごとに写す必要がない。

**限界**: 不変条件をモデル関数の中に正しく書く責任は残る。縛れるのは「迂回して store を書くこと」と「不変条件があることを忘れること」である。

---

## 3. 「編集」と「担当の変更」の権限の分離（観測: 007・013・008 は正解、ただし写し方しだい）

**観測**
- README のルールは「contributor は自分の担当タスクなら編集できるが、担当の変更はできない」。
- 一括ステータス変更（001）は「自分のタスクなら可」を正しく写した。
- 一括担当変更（007）で予想していた罠は、001 の「自分のタスクなら可」をそのまま写すことだった。Haiku は回避した（manager 以外は 403）。
- PATCH での付け替え（013）でも、contributor が担当を変えると 403 だった。
- どれも「写したロジックが偶然正しかった」だけで、構造的な保証はない。

**縛り方**
```go
type EditTask interface {        // manager、または「自分が担当」の contributor
    View() TaskView
    SetTitle(string) error
    SetStatus(Status) error
}
type AssignTask interface {      // manager だけが Acquire できる。EditTask とは別の型
    EditTask
    Reassign(AssigneeIn) error   // 1. の AssigneeIn を要求するので、担当者の検証も同時に強制される
}
func (a *Authorizer) AcquireEdit(actor, project, task string) (EditTask, error)
func (a *Authorizer) AcquireAssign(actor, project, task string) (AssignTask, error)
// 一括は全件まとめて取得する。1件でも失敗すれば capability を返さないので、原子性も型で表せる。
func (a *Authorizer) AcquireAssignAll(actor, project string, ids []string) ([]AssignTask, error)
```
- case-3 の `ReadInvoice` と `EditInvoice` の分離と同じ発想である。
- `EditTask` には `Reassign` が存在しないので、contributor の経路で担当変更を書くとコンパイルが通らない。
- 一括操作の all-or-nothing は、「全件の Acquire に成功したら書き込む」という形に自然に落ちる。001・007・014 が都度手で書いていたものである。

---

## 4. 横断クエリと「どこからでも呼べる」store 関数（観測: 006 は正解、採点役が潜在リスクを指摘）

**観測**
- 006（`GET /me/tasks`）で Haiku は `store.TasksAssignedTo(user)` を追加した。
- ハンドラ側では `listTasks` と同じ「読めるか」の確認を、5回目のコピーとして各タスクに適用し、正解した。
- ただし `TasksAssignedTo` 自体は認可なしで全プロジェクトを走査する。将来ほかの呼び出し元が直接使えば、そのまま漏れる。

**縛り方**
```go
// 「この actor が読めるプロジェクト」の集合を、Transformer（または Authorizer）だけが作る。
type ReadableProjects interface {
    // 読める範囲に絞った結果しか返さない。生の store 走査はこの実装の中だけで行う。
    TasksAssignedTo(user string) []TaskView
}
func (a *Authorizer) AcquireReadable(actor string) (ReadableProjects, error)
```
- 横断クエリのモデル関数を、読み取り権限オブジェクトのメソッドとして定義する。認可なしの走査関数は公開しない。
- 「認可チェックをどこに写すか」という問題自体がなくなる。

---

## 5. ポリシー変更を 1か所に集める（観測: 002/003 の org admin 変更、同じ確認のコピーが最大5か所）

**観測**
- 002/003（「org admin は manager と同じように書ける」）で、Haiku は `effectiveRole()` を自発的に作り、5つのハンドラすべてに適用して正解した。
- ただしこれは Haiku がたまたまヘルパーにまとめたからである。if 文を写す形のコードベースでは、ハンドラが増えるたびに同じ確認のコピーが増える。006 で5回目、008 で4回目、013 では PATCH の中に移動先の確認が追加された。
- 方針変更のときに「1か所写し忘れる」危険は、トライアルを重ねるほど大きくなる。

**縛り方**
```go
// プロジェクト単位の Transformer が、actor とプロジェクトから認可オブジェクトを1回だけ作る。
// ハンドラが受け取るのはロールの文字列ではなく、AcquireXxx の結果である。
tanukirpc.RouteWithTransformer(r, projectTransformer(authz), "/projects/{pid}", func(r *tanukirpc.Router[*projectRegistry]) {
    // projectRegistry には ReadProject / WriteProject / ManageMembers のうち、取得できたものだけが入る
})
```
- 「org admin を manager とみなす」は `Authorizer` の判定関数 1か所の変更になる。全ハンドラが自動で従う。
- ハンドラが `role != RoleManager` のような比較を書く場所そのものがなくなる。

**限界**: Transformer がパスのプロジェクトしか扱わない場合、移動先・複製先のように**ボディで指定されるプロジェクト**は、モデル関数の中で `AcquireWrite(dstID)` を呼ぶ必要がある（1. の `WriteProject`）。このときも「Acquire しないと引数を作れない」形にしておけば、取得し忘れはコンパイルで止まる。

---

## まとめ: 何が型で止まり、何が残るか

| # | 観測した／指摘された誤り | 型で止まるもの | 残るもの |
|---|---|---|---|
| 1 | 移動先の担当者ルールの欠落（6/6） | `AdmitAssignee` を呼ばない移動 | 取得した `AssigneeIn` の使い回し |
| 2 | 降格による不変条件違反（015） | store の直接更新、担当タスクの扱いの選び忘れ | `ChangeRole` 内部の実装の正しさ |
| 3 | 編集と担当変更の混同（罠として想定） | contributor 経路での担当変更、一括操作の部分適用 | Acquire の判定ロジック自体 |
| 4 | 認可なし走査関数の流用（潜在） | 読み取り範囲を絞らない横断クエリ | ― |
| 5 | 方針変更時の写し忘れ（コピー最大5か所） | ハンドラごとのロール比較 | Authorizer の判定ロジック自体 |

どれも「ハンドラから生の store に届かない」ことが前提である。case-3 で、`reg.tenant undefined` や `capability.editableInvoice undefined` がコンパイルエラーになったのと同じ性質を使っている。
