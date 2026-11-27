# 追加実験: README に不変条件を書く効果と、構造の効果を分ける（2026-11-24）

## 動機

スライドの独立レビューで、SQLite 版の比較（if-sql と rel-sql）には交絡があると指摘された。

- rel-sql の README とコメントには、モデルルール（不変条件）が書いてある。
  - "tasks belong to existing projects; an assignee is a contributor or manager of the task's project; project members belong to the project's organization"
- if-sql の README には書いていない。

このままでは「構造の効果」と「不変条件を書いた効果」を分けられない。そこで両方向のアブレーションを行い、2×2 で並べた。

## 条件

| | 不変条件の記述なし | 不変条件の記述あり |
|---|---|---|
| if 文版 | `base-v2-sql`（既存 441〜 / 526〜） | **`base-v2-sql-rules`（606〜625）** |
| 構造版（rel） | **`base-rel-sql-norules`（626〜645）** | `base-rel-sql`（既存 481〜 / 566〜） |

- `fixture/taskboard-v2-sql-rules`:
  - README の Access rules の末尾に、rel-sql と同じ意味のモデルルール3つと「破る書き込みは 400 で拒否し、何も変えない」を足した。
  - if 文版には関係メソッドがないので、`AssignedTasks` の名前は入れていない。
  - README 以外は v2-sql と同一。差分は `results/v2-sql-rules-readme.diff`。
- `fixture/taskboard-rel-sql-norules`:
  - README からモデルルールの文と "validates the model rules on them" を消した。
  - `internal/model/aggregate.go` の冒頭コメントからも、ルールの一覧（"Model rules (see README): ..."）を消した。
  - 関係メソッドの一覧（`membership.AssignedTasks(ctx, db)` など）とコード本体はそのまま。差分は `results/rel-sql-norules.diff`。
  - エラー文字列 "assignee must be a contributor or manager of the project" は、if 版にも同じものがあるので残した。
  - 消す範囲はユーザーがレビューして決めた。
- タスクは降格系の2つ: 015（ロール変更）と 025（ロール一括変更）。各条件・各モデル5本ずつ。
- 採点器は `015-200` / `025-200`。`SHARED_GOMODCACHE` / `WARM_GOCACHE` は SQLite 版と同じ。
- 新しい40本の driver ログはすべて DONE で終わり、未コミットの変更はなかった。40本とも valid_run だった。
- 結果のファイル:
  - 対応表: `results/{v2-sql-rules,rel-sql-norules}-manifest.tsv`
  - 判定: `results/*-verdicts/verdicts.jsonl`
  - 試行ごとの diff・サマリ: `results/structure/trials/{606..645}.*`

## 結果（authz_ok、ロール変更 /5 + 一括変更 /5 = /10）

| モデル | if × ルールなし | if × ルールあり | 構造 × ルールなし | 構造 × ルールあり |
|---|---|---|---|---|
| Haiku | 0 + 0 = **0** | 4 + 3 = **7** | 0 + 0 = **0** | 4 + 2 = **6** |
| Sonnet | 2 + 0 = **2** | 4 + 1 = **5** | 4 + 3 = **7** | 5 + 5 = **10** |

1セルは5本しかない。差が1〜2本のものはノイズの範囲として読むこと。

## 読み取れること

- **Haiku では、効いているのは README の不変条件で、構造そのものではない。**
  - ルールを消すと、構造版でも 0/10 だった。if 版にルールを書くと 7/10 で、構造版のルールあり（6/10）と同程度になった。
  - 以前の rel-sql と if-sql の差（Haiku 6/10 対 0/10）は、ほぼ全部が不変条件を書いた効果だった。
- **Sonnet では両方が効き、組み合わせたときだけ満点になる。**
  - 構造だけ（7/10）とルールだけ（5/10）は、どちらも何もなし（2/10）より良い。両方そろうと 10/10 になった。
  - Sonnet は関係メソッド `AssignedTasks` を見て、自分で担当の整理に気づくことがある。Haiku はそうしなかった。
- **合否と「担当者に触れたか」は完全に一致した。**
  - 新しい40本では、合格した試行はすべて担当を外すか、降格を拒否していた。
    - 構造版は全合格が `AssignedTasks` を呼んでいた。
    - if 版は SQL で担当を外すか、拒否していた。Haiku 611 は、担当が viewer に残る変更を 400 で拒否した。
  - 失敗した試行は、本番コードに担当を扱う行が1行もない（if × ルールあり、構造 × ルールなしの両方）。
- **失敗した試行も、気づいていた場合がある。** 最終報告で触れたのに直さなかった例:
  - if × ルールあり:
    - Sonnet 617: 「降格しても assignee は変わらない。自動で外すかは決めていないので、必要なら教えて」
    - Sonnet 623: 「既存の『assignee は contributor か manager のみ』というルールとは食い違う状態が残る」と明記したうえで、対応はユーザーに委ねた。
  - 構造 × ルールなし:
    - Haiku 627: "A demoted member keeps their assigned tasks. They just can't edit them."
    - Haiku 630: 降格した contributor の担当が残ることを "A consequence to be aware of" として報告した。
    - Sonnet 641: 担当者の降格を拒否する案を考えた。しかしタスク文の例（t1 を持つ ben を降格する）が失敗するので、その案を捨てた。最終報告では「viewer が担当のまま残る」ことを Open issue として挙げた。
  - それ以外の失敗（Haiku 606、612、613、626、628、629、631〜635、Sonnet 622、624、625、636、642）は、担当にまったく言及していない。
    - Haiku 631〜633 は "assignee" という語を、「メンバーでないユーザーへの 400 は、assignee のチェックと同じ扱い」という文脈でだけ使った。
  - つまり失敗の一部は「気づいたが、仕様にないので直さなかった」型だった。不変条件が README に書いてあっても、Sonnet は2本でそうなった（617、623）。

## 注意

- 対象は降格系の2タスクだけ。ほかの6タスク（移動、削除など）で同じ分解をしたわけではない。
- フィクスチャ、タスク、採点器は同じ側が作った。この交絡は残っている。
