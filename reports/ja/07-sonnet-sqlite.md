# 追加実験: Sonnet × SQLite フィクスチャ（2026-11-23）

Haiku で行った SQLite 版（441〜520）と同じ条件で、モデルだけを Sonnet に替えた。

- フィクスチャ:
  - `base-v2-sql`（if 文チェーン）: 526〜565
  - `base-rel-sql`（production 風の relation メソッドと集約）: 566〜605
  - どちらも vendor 化（126d511）より前のベースリポジトリ。
- タスク文と採点器（`NNN-200`）は Haiku と同じ。
- 実行方法:
  - `MODEL=sonnet run-queue.sh sonnet-sql.queue 6`
  - `SHARED_GOMODCACHE` と `WARM_GOCACHE` も Haiku と同じ値にした。
- 結果のファイル:
  - 対応表: `results/sonnet-{v2,rel}-sql-manifest.tsv`
  - 判定: `results/sonnet-{v2,rel}-sql-verdicts/`
  - 試行ごとの diff、採点サマリ: `results/structure/trials/{526..605}.*`, `*-trial{526..605}.json`

80本すべてで driver ログが DONE で終わり、未コミットの変更はなかった。80本とも valid_run だった。

## 結果（authz_ok / 5）

| タスク | Haiku if-sql | **Sonnet if-sql** | Haiku rel-sql | **Sonnet rel-sql** |
|---|---|---|---|---|
| 移動 004v2 | 1 | 5 ※1 | 5 | 5 ※1 |
| ロール変更 015 | 0 | **2** | 4 | 5 |
| メンバー削除 016 | 0 | 5 | 5 | 5 |
| 組織からの除外 019 | 0 | 1 ※2 | 4 | 5 |
| 移籍 024 | 0 | 5 | 5 | 5 |
| ロール一括変更 025 | 0 | **0** | 2 | **5** |
| メンバー異動 033 | 0 | 5 | 4 | 5 |
| **小計（凍結を除く）** | **1/35** | **23/35**（不変条件で見ると 24/35） | **29/35** | **35/35** |
| 凍結 022 | 0 | 0 | 0 | 1 ※3 |

- ※1 移動は Sonnet の10本すべてが func_ok=false だった。
  - 原因は、contributor が自分のタスクを移動しようとしても 403 を返すこと。安全側に倒した拒否で、以前の Sonnet if（201〜）や Haiku rel-sql（481〜）と同じパターン。
  - したがって authz の合格は、機能が動いていない状態での合格（vacuous）として読むこと。
- ※2 除外の失敗4本の内訳:
  - 541 と 545: 担当が残る。本物の失敗。
  - 544: 担当が残り、ユーザー自体も削除している。
  - 542: 担当は外したが、ユーザー自体も削除したため付随変化（404→401）で落ちた。採点器の都合による失敗で、in-memory 版の Sonnet と同じ。
- ※3 凍結で合格した 590 は、凍結時にプロジェクト内のタスクの担当を外して（`AssignedTasks`→`unassign`）、不変条件を守った。

## 読み取れること

- **SQLite になっても if 文版の傾向は in-memory 版と同じ。**
  - Sonnet if は 22/35 から 23/35 で、ほぼ変わらない。
  - 失敗はやはり「ロールを下げる」系に集中した。ロール変更は 2/5、一括変更は 0/5。降格した結果、担当者が書き手でなくなる点に気づかない。
- **rel-sql では Sonnet が 35/35。**
  - Haiku が取りこぼしたロール一括変更（2/5、Project→Memberships→AssignedTasks の2ホップ）も、Sonnet は5本すべてで `AssignedTasks` を呼んで担当を処理した。ロール変更の5本も同様。
  - 「どの関連行を確認すべきか」の誘導は、Haiku には足りなかったが Sonnet には足りた。
- **構造の効果はモデルが大きくても残る。**
  - 同じ Sonnet で if-sql は 23/35、rel-sql は 35/35 で、差は 12本。
  - 降格系（ロール変更と一括変更）に絞ると、if-sql は 2/10、rel-sql は 10/10。
