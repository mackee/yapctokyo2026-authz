# yapctokyo2026: コーディングエージェントは認可をどう間違えるか

[English](README.md)

YAPC::Tokyo 2026 の発表のための実験資料。

問いは次のとおり。既存の Web アプリにコーディングエージェントが機能を足すとき、**リソース単位の認可**を正しく書けるか。間違えるなら、アプリの構造によって結果は変わるか。

- **アプリ**: `taskboard`。Go の小さな HTTP API で、org、project、メンバーシップ（manager / contributor / viewer）、担当者付きのタスクを持つ。
  - HTTP から見た振る舞いが同じで、作りだけが違う版をいくつか用意した。手で写した `if` 文、capability と全体の不変条件チェック、集約、関係メソッド、if 文版と関係メソッド版の SQLite 版などである。
- **実装役**: Claude Haiku と Claude Sonnet。自動運転で動かした（`claude --model … --permission-mode dontAsk`）。各トライアルは専用のクローンの中で、ネットワークのない OS サンドボックスの中で動く。
- **採点**: 隠しテスト（Go）。実装を見る前に書いて凍結し（SHA-256 を記録）、両方のベースライン、参照実装、ミュータントで検証した。
  - `authz_ok`: AUTHZ（拒否すべき操作を拒否する）と ATOM（拒否時に何も変わらない、不変条件が保たれる）のサブテストに FAIL がない。
  - `func_ok`: 機能が動く。
  - **拒否しすぎ**: `authz_ok` だが `func_ok` ではない。安全だが、機能として動いていない。
- 全部で約 **1,840 トライアル**。ほとんどのセルは5本ずつ。

## 実験

各レポートは英語版と日本語版があり、内容は同じ。

| # | レポート（ja / en） | トライアル | 問い |
|---|---|---|---|
| 01 | [初期トライアル](reports/ja/01-early-trials.md) / [en](reports/en/01-early-trials.md) | 001〜015 | if 文版で Haiku に1本ずつ解かせる。どこで間違えるか |
| 02 | [トライアルのブランチ](reports/ja/02-trial-branches.md) / [en](reports/en/02-trial-branches.md) | 001〜015 | 初期トライアルを `haiku-trials/NNN` ブランチから再現する方法 |
| 03 | [構造 対 if 文](reports/ja/03-structure-vs-if.md) / [en](reports/en/03-structure-vs-if.md) | 016〜160（+ 005、009〜012、015） | if 文版と、tanukirpc + capability + 全体の不変条件チェック版の比較（Haiku） |
| 04 | [Sonnet とガイドのアブレーション](reports/ja/04-sonnet-and-guide-ablation.md) / [en](reports/en/04-sonnet-and-guide-ablation.md) | 201〜320 | モデルを大きくすれば差は埋まるか。README の「機能の足し方」は効いているか |
| 05 | [集約と関係メソッド](reports/ja/05-aggregate-and-relation-methods.md) / [en](reports/en/05-aggregate-and-relation-methods.md) | 321〜400 | 全体チェックのない、実運用に近い構造 |
| 06 | [実運用寄せと SQLite](reports/ja/06-production-style-and-sqlite.md) / [en](reports/en/06-production-style-and-sqlite.md) | 401〜525 | 認可オブジェクト + `*Table` + SQLite（Haiku） |
| 07 | [Sonnet × SQLite](reports/ja/07-sonnet-sqlite.md) / [en](reports/en/07-sonnet-sqlite.md) | 526〜605 | 同じ条件を Sonnet で |
| 08 | [README の不変条件のアブレーション](reports/ja/08-readme-invariant-ablation.md) / [en](reports/en/08-readme-invariant-ablation.md) | 606〜645 | 効いているのは構造か、README に書いた不変条件か |
| 09 | [正常系のテストと仕様文](reports/ja/09-happy-path-tests.md) / [en](reports/en/09-happy-path-tests.md) | 646〜805 | 許可側だけのテーブルテストと、許可側の仕様文で、拒否しすぎは減るか |
| 10 | [仕様文とテストの切り分け](reports/ja/10-split-sentence-vs-test.md) / [en](reports/en/10-split-sentence-vs-test.md) | 806〜1127 | 効いたのはどちらか |
| 11 | [Quint モデル](reports/ja/11-quint-model.md) / [en](reports/en/11-quint-model.md) | 1128〜1447 | 仕様と不変条件を Quint でモデル化し、エージェントにモデルの更新と検査をさせたらどうなるか |
| 12 | [不変条件のテストと TDD](reports/ja/12-invariant-tests-and-tdd.md) / [en](reports/en/12-invariant-tests-and-tdd.md) | 1448〜1927 | 不変条件を確かめるテストのヘルパーを置くと、TDD を指示すると、どうなるか |
| A1 | [型で縛る例](reports/ja/A1-tanukirpc-binding-examples.md) / [en](reports/en/A1-tanukirpc-binding-examples.md) | — | tanukirpc の Transformer と型で、これらの規則を縛る案 |

## わかったこと

特に断りがなければ、数は `authz_ok` の本数である。「7タスク」は、エンティティをまたぐ次の7つのタスクを指す。移動、ロール変更、メンバー削除、組織からの除外、移籍、ロール一括変更、メンバー異動。凍結は別に数える。

1. **if 文を写す書き方では、呼び出し元の権限チェックは正しく書けるが、エンティティをまたぐ不変条件を壊す。**
   - 「呼び出し元は manager か、プロジェクトはアーカイブ済みか」は正しく写せる。7タスクで、if 文版に AUTHZ（呼び出し元のチェック）の失敗は1本もなかった。
   - 間違えるのは、新しい書き込み口が、別のエンティティが持つ規則を壊す場合である。例: タスクを移動すると、移動先で書けない担当者が残る。メンバーを降格したり外したりしても、担当が残る。
   - 7タスクで、Haiku は in-memory 版で 0/35、SQLite 版で 1/35 だった。
   - Sonnet は 22/35 と 23/35 だった。Sonnet の失敗には、不変条件は守っていて採点器側の都合（組織からの除外でユーザーを消した）で落ちたものが含まれる。不変条件だけで数えると 27/35 と 24/35 だった（レポート 04、07）。
   - 降格系（ロール変更 + 一括変更）は Sonnet でも弱く、in-memory 版でも SQLite 版でも 2/10 だった。SQLite 版では組織からの除外も 1/5 だった。
   - 読み取り系の機能や、権限の判断が明示的な機能は、if 文版でも正しかった。ただし Haiku で各1本ずつである（レポート 01、03）。
2. **構造は効く。効き方は2通りある。**
   - capability と、コミット時の全体の不変条件チェックがある版（`cap`）では、両モデルとも 35/35 だった。これは `authz_ok` の数で、機能として動いたのは Haiku 27/35、Sonnet 30/35。Haiku の移動5本は、すべて移動を拒否して（拒否しすぎで）合格していた。
   - 全体チェックがない版では、試したどの構造でも Haiku は 29〜30/35 だった（集約、関係メソッド、認可オブジェクト + `*Table` の in-memory 版と SQLite 版）。Sonnet は最後の SQLite 版だけで回し、35/35（func は 30/35）だった。
   - 関係メソッドのある版では、成否は、メンバーの担当タスク（`AssignedTasks`）を読み込んだかどうかと完全に一致した。起点から1段なら Haiku も使う。2段（ロール一括変更）だと、Haiku はほとんど使わず（1〜2/5）、Sonnet は使った（5/5）。
3. **Haiku では、効果の大半は不変条件を書いておいたことによる。**
   - 構造版の README から不変条件を消すと、Haiku は降格系で 0/10 に落ちた。
   - if 文版の README に不変条件を書くと、7/10 になった。
   - Sonnet は構造と記述の両方が効き、両方そろったときだけ 10/10 になる。
4. **仕様が不変条件と衝突すると、エージェントは不変条件のほうを弱めがちである。**
   - プロジェクトの凍結はメンバー全員を viewer にするので、「担当者は書き手」と衝突する。
   - `cap` 版では、全体の不変条件チェックを書き換えて例外を作った。Haiku 4/5、Sonnet 4/5、「機能の足し方」ガイドなしの Haiku 5/5。どれも落ちた。
   - Quint モデルのある条件では、凍結の40本のうち8本が Quint の不変条件を書き換え、8本とも落ちた。反例を見ていたのは8本中2本だけで、残りは最初から弱めていた。それ以外はおおむね不変条件を守った。モデルの更新を必須にした条件 B では、凍結の 16/20 が `authz_ok` だった。
   - 不変条件をテストのヘルパー（`rules_test.go`）で確かめる条件でも、凍結の40本のうち7本（すべて Haiku）がその規則に「凍結中は対象外」という例外を足し、7本とも落ちた。チェックが落ちてから緩めたのは1本だけだった（レポート 12）。
5. **拒否しすぎは別の失敗で、テストだけでは直らない。**
   - タスクの移動を manager だけに絞ることが多かった。SQLite 版の比較（実験 06〜07）では、移動の20本中15本がそうだった。
   - タスク文に許可側の1文（「両方のプロジェクトで contributor なら、自分のタスクを移動できる」）を足すと、拒否しすぎは 16 本から 2 本に減った。ただし if 文版では、失敗の種類が入れ替わっただけだった。if 文版の移動10本はすべて移動を許可し、移動先で書けない担当者を残した。また、この1文は採点器が確かめる内容をほぼそのまま書いたものに近い（レポート 10）。
   - 許可側だけのテーブルテストをリポジトリに置いても効かなかった（16 → 14）。エージェントが足した行は仕様ではなく、自分の実装を写したものだった。
   - TDD を指示しても減らなかった（16 → 18）。先に書いたテストに「manager だけが移せる」を期待値として書き、拒否しすぎを固定した試行が多かった（レポート 12）。
6. **形式モデルは不変条件には効くが、モデル自体の誤りには効かない。**
   - Quint モデルに操作を足し、`quint run --invariant` を通すことを必須にすると（条件 B）、4セルとも `authz_ok` が 35/35 になった。1/35 だった if 文版 × Haiku も含む。
   - モデルを置くだけ（条件 A、更新は必須にしない）でも、if 文版は伸びた。Haiku 1 → 18/35、Sonnet 23 → 33/35。
   - 反例を見たトライアルは少なかった（B で多くても160本中20本）。効いたのは、式で書かれた不変条件に対してアクションを書かせたことだった。
   - 拒否しすぎは減らなかった（16 → 19）。「manager だけ」がモデル自体に書かれていて、不変条件の検査では見えない。
   - モデルの不変条件に入っていない規則（アーカイブ済みのチェック、トライアル 1310）は、検査を素通りして採点で落ちた。
7. **不変条件のテストは効くが、効いたのは「規則が読めること」で、TDD だけでは効かない。**
   - テストの共通ヘルパー `do()` が、書き込みのたびに3つの不変条件を DB で確かめるようにした。それだけで、if 文版 × Haiku は 1 → 31/35、Sonnet は 23 → 35/35、rel 版は両モデルとも 35/35 になった。Quint B とほぼ同じである。
   - ただし、途中の `go test` でチェックが実際に落ちたのは320本中1本だけだった。どのトライアルも `rules_test.go` を読んでいた。エージェントは規則を先に読み、それに合わせて実装していた。
   - テストが捕まえるのは、エージェントが試した場面だけである。if 文版 × Haiku の移動は、ヘルパーがあっても 4/5 が漏れた。ivy が担当のタスクを移す場面を、どのテストも作っていなかった。
   - TDD を指示すると、320本すべてがテストを先にコミットした。それでも結果は最初の比較とほぼ同じだった（if 文版 × Haiku 1 → 2/35、Sonnet 23 → 22/35）。先に書くテストは、エージェントがすでに理解している範囲を写すだけで、if 文版 × Haiku が担当の後始末をテストしたのは降格系・移動・凍結の30本中1本だった（レポート 12）。

## 注意

- ほとんどのセルは5本しかない。1〜2本の差はノイズとして読むこと。
- fixture、タスク、採点器は同じ側（作者と Claude）が作った。採点器は凍結して検証したが、独立した評価ではない。
- 採点器が検出できるのは、テストが覆っている範囲だけである。採点器 029 の注記など、各レポートを参照のこと。
- すべて1台の macOS 上で、特定のハーネス（herdr、Claude Code）を使って実行した。ほかの環境で新しいトライアルを回すには調整が要る。[REPRODUCE.ja.md](REPRODUCE.ja.md) を参照。

## リポジトリの構成

| パス | 中身 |
|---|---|
| `reports/{ja,en}/` | 上のレポート |
| `fixture/` | トライアルのベースにしたアプリの全版。`fixture/quint/` は Quint モデル。`taskboard-cap-noguide` と `taskboard-rel-v1` は、現在の fixture と一致しなくなったベースのスナップショット |
| `tasks/` | エージェントにそのまま渡したタスク文（日本語）。`tasks/happy/`、`tasks/notest/`、`tasks/quint/`、`tasks/tdd/` は実験 09〜12 の変種 |
| `results/*-manifest.tsv` | トライアル → 構成、タスク、採点器、中心のサブテスト |
| `results/*-verdicts/verdicts.jsonl` | トライアルごとの判定（`authz_ok`、`func_ok`、落ちたサブテスト） |
| `results/structure/trials/` | トライアルごとの提出物の差分（`<id>.diff`）と採点結果（`<grader>-trial<id>.json`） |
| `results/structure/graders/`、`graders-200/`、`results/NNN/` | 凍結済みの採点器（`zz_grader_test.go`、`FROZEN.sha256`、期待するサブテスト） |
| `results/trial-bases.tsv` | トライアル → fixture → fixture の Git tree SHA |
| `results/run-status.tsv` | 各実行の終わり方（DONE / NO COMMIT / 起動せず、未コミットの変更、再実行）。ドライバのログから取り出したもの |
| `results/queues/` | トライアル 201 以降を回したキューファイル（パスはテンプレート化） |
| `results/quint-usage.tsv`、`*-testrows.tsv`、`invtest-rulecheck-hits.tsv`、`tdd-first-commit-tests.tsv` | 実験 09〜12 の追加集計（Quint の使い方と規則のチェックの失敗は会話記録から、テストの行と TDD の最初のコミットは差分から数えた） |
| `.claude/skills/haiku-attempt/` | ハーネス。実行・採点・監査のスクリプトと、つまずきどころのまとめ（`SKILL.md`） |
| `scripts/` | `regrade-trial.sh` はトライアルを1本作り直して採点し直す。`summarize-verdicts.py` は結果の表を出す |
| ブランチ `haiku-trials/001`〜`015` | 初期トライアル。タスク、設定、採点結果を含む |

## 再現

[REPRODUCE.ja.md](REPRODUCE.ja.md)（[en](REPRODUCE.md)）を参照。3段階ある。

1. 判定から表を計算し直す。
2. fixture と差分から任意のトライアルを作り直し、採点し直す。
3. ハーネスで新しいトライアルを回す。

## 公開していないもの

Claude Code の生ログ、つまりエージェントのセッションの会話記録とドライバのログ（各実行の端末画面の写し）は、このリポジトリに入れていない。実行者の環境（アカウント情報、システムプロンプト、ローカルのパス）を含み、結果を確かめるのには要らないからである。レポートがそこから取り出したものは、集計表として置いてある。

- `results/run-status.tsv`: 各実行の終わり方。ドライバのログから取り出した。
- `results/quint-usage.tsv`: トライアルごとの Quint の使い方（実行したコマンド、見た反例）。会話記録から数えた。
- `results/invtest-rulecheck-hits.tsv`: トライアルごとの `go test` の回数と、そのうち不変条件のチェックが落ちた回数。会話記録から数えた。

それ以外に、レポートのうち会話記録から数えた記述には、その旨を書いてある。
