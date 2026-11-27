# haiku-attempt: 成功例と失敗例（再現用）

Haiku（`claude --model haiku`）に、if 文を並べた素朴な Go アプリ（taskboard）の機能追加を頼んだトライアルの記録。
各トライアルはブランチ `haiku-trials/NNN` に保存してある。

## ブランチの構成

| コミット | 中身 |
|---|---|
| 1つ目 | ベース（`fixture/taskboard` または `fixture/taskboard-v2` と同一） |
| 2つ目 | Haiku の提出物（作業ツリー。`.git` は除く）と、メタデータの `.trial/` |

`.trial/` の中身:
- `task.md`: Haiku に渡したタスク文（原文そのまま）
- `haiku-settings.json`: Haiku に渡した設定（権限、サンドボックス、環境変数）
- `meta.json`: 判定、モデル、fixture、タスク、採点器、採点結果のパス
- `grade-summary.json`: 凍結済み採点器による採点結果

元のブランチには、ドライバのログと Haiku のセッションの会話記録も入っていた。これらは公開していない。ドライバのログから取り出した実行状況は `results/run-status.tsv` にある。

ブランチは、トライアルの `.git`（信頼できない）からは取り込んでいない。作業ツリーのファイルから作り直したものである。

## 再現手順

**1. 採点だけを再現する（決定的）**
ブランチを展開し、凍結済み採点器にかける。採点器は `results/<grader>/` にある（`FROZEN.sha256` でハッシュを確認する）。
```sh
X=$(mktemp -d /private/tmp/repro.XXXXXX)
git archive haiku-trials/015 | tar -x -C "$X" && rm -rf "$X/.trial"
results/015/run.sh "$X" /private/tmp/repro-015.json   # go は sandbox-exec 内で実行される
jq '{integrity_ok, failures: [.failures[].name]}' /private/tmp/repro-015.json
```
015・009・013 で試し、記録と同じ FAIL の集合になることを確認済み。

**2. 実装からやり直す（Haiku の出力は確率的）**
このリポジトリのハーネスを使う。
```sh
FIXTURE=<fixture dir> BASE_REPO=~/src/haiku-probe/base-v2 .claude/skills/haiku-attempt/scripts/setup-base.sh
BASE_REPO=~/src/haiku-probe/base-v2 .claude/skills/haiku-attempt/scripts/run-trial.sh <新しいID> tasks/<task>.md
results/<grader>/run.sh ~/src/haiku-probe/trials/<新しいID> <out.json>
```
必要なもの: herdr の中で実行すること、`/opt/homebrew/bin/go`、macOS の `sandbox-exec`。注意点は SKILL.md の Pitfalls を参照。

## 失敗例

### F1. 移動先の担当者ルールの欠落（移動タスク 6/6）
- **タスク**: `POST /projects/{pid}/tasks/{tid}/move {"toProjectId":...}`（`tasks/004-move-task.md`。005 も同文）。
- **誤り**: 移動先の 404/403/409 は確認するが、「担当者は移動先の contributor/manager でなければ 400」を確認しない。`task.ProjectID = target.ID` で終わる。
- **再現する入力**: ivy が t1（担当 ben）を web から ops に移す → **200 になり、ben が ops の担当者のまま残る**（正しくは 400 で何も変わらない）。

| ブランチ | シード | 採点器 | 結果 | 補足 |
|---|---|---|---|---|
| `haiku-trials/004` | v1 | 004（133件） | 133/133 PASS | v1 シードでは到達できない（潜在）。Haiku 自身が README に「再チェックしない」と明記 |
| 同上（v2 シードを接ぎ木） | v2 | 004v2（185件） | **4件 FAIL** | `results/004v2/004v2-trial004-grafted.json` |
| `haiku-trials/005` | v2 | 004v2 | **22件 FAIL** | 担当者ルール欠落（4件）に加えて、移動を manager 限定にした（安全側の制限しすぎ、18件）。自分のテストで ivy の t1 移動を 200 と期待している |
| `haiku-trials/009` | v2 | 004v2 | **4件 FAIL** | 担当者ルール欠落のみ |
| `haiku-trials/010` | v2 | 004v2 | **4件 FAIL** | 同上 |
| `haiku-trials/011` | v2 | 004v2 | **22件 FAIL** | 005 と同じパターン（manager 限定） |
| `haiku-trials/012` | v2 | 004v2 | **22件 FAIL** | 同上 |

FAIL するサブテスト: `ATOM_/CONTRACT_/VALID_IvyAssigneeNotWriterInDest` と `VALID_IvyAssigneeViewerInDest`。

### F2. ロール降格による不変条件違反（015）
- **ブランチ**: `haiku-trials/015`。採点器は 015（100件）、**ATOM 6件 FAIL**。
- **タスク**: `PUT /projects/{pid}/members/{uid} {"role":...}`（`tasks/015-change-member-role.md`）。
- **誤り**: 呼び出し元の確認（manager のみ、アーカイブ済みは 409、対象はメンバー）は正しい。しかし store に `SetRole`（`s.members[p][u] = role` の直接代入）を足して呼ぶだけで、担当タスクを見ていない。
- **再現する入力**: ivy が web で ben を viewer に降格 → **200 になり、ben は「viewer なのに t1 の担当者」になる**。cat を ops で降格しても同じ（t4）。
- FAIL するサブテスト: `ATOM_DemoteAssigneeToViewer_{benWeb,benWebByManagerAnn,catOps,catWeb}`、`ATOM_DemotedManagerAssignee_ivy`、`ATOM_DemoteViaBothPathsNoViewerAssignees`。

### 参考: ハーネス側の失敗（モデルの結果ではない）
- `haiku-trials/002`: 許可リストが狭すぎて Bash がすべて拒否され、テストもコミットもされなかった（SKILL.md P4b）。未採点。
- 003 と 004 は、テスト通過後にコミットだけ失敗した（サンドボックス内で `~/.gitconfig` が読めない、`.git/**` の拒否ルールが `index.lock` を塞いだ）。作業ツリーを採点した。

## 成功例

| ブランチ | タスク | 採点器 / 結果 | 正しかった理由（コードから） |
|---|---|---|---|
| `haiku-trials/001` | 一括ステータス変更 | 001 / 98/98 | `createTask` の確認を同じ順番で写し、全件を確認してから書き込んだ |
| `haiku-trials/003` | org admin を manager 扱いにする方針変更 | 002 / 151/151 | `effectiveRole()` を自発的に作り、5つのハンドラすべてに適用した |
| `haiku-trials/006` | プロジェクト横断の「自分のタスク」 | 006 / 80/80 | ユーザーは `X-User` からのみ取る。各タスクを `listTasks` と同じ「読めるか」の確認に通した |
| `haiku-trials/007` | 担当者の一括変更 | 007 / 161/161 | manager 以外は 403（001 の「自分のタスクなら可」を写さなかった）。担当者もプロジェクトごとに検証した |
| `haiku-trials/008` | 別プロジェクトへの複製 | 008 / 124/124 | 移動先で `createTask` の確認を担当者ルールまで丸ごと実装した。F1 との対照 |
| `haiku-trials/013` | PATCH に `projectId` を足して付け替え | 013 / 155/155 | 既存 PATCH の担当者検証を移動先プロジェクトに向け直した（`s.store.Role(target.ID, resultAssignee)`）。F1 と同じ操作でも、既存の検証を通る形なら正しくできた |
| `haiku-trials/014` | プロジェクト統合（一括移動） | 014 / 105/112 | 担当者を全件、統合先で検証し、原子的に処理した。FAIL 7件はすべて CONTRACT（400 のところで 409 を返した）で、安全側 |

## F1 と成功例の対比から言えること
- 同じ「移動先での担当者の検証」でも、**既存の作成・更新の検証を通る形**（008 の複製、013 の PATCH 付け替え、014 の統合）では正しく、**新しいハンドラで `ProjectID` だけを書き換える形**（F1 の move）では6本中6本で抜けた。
- F2 も同じ構造である。新しく書き込み口（`SetRole`）を作ると、既存の検証経路にない不変条件が抜ける。
- どちらも、型で要求するモデル関数で縛れる例として [`A1-tanukirpc-binding-examples.md`](A1-tanukirpc-binding-examples.md) の 1・2 に対応する。
