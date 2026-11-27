# 実験の再現

[English](REPRODUCE.md)

再現には3段階ある。段階が上がるほど準備が増える。

| 段階 | 得られるもの | 必要なもの |
|---|---|---|
| 1. 表を計算し直す | アーカイブ済みの判定から、レポートの数字を出す | Python 3 |
| 2. アーカイブ済みのトライアルを採点し直す | 各トライアルを fixture と差分から作り直し、凍結済みの採点器にかける | macOS（`sandbox-exec`）、Go、Python 3 |
| 3. 新しいトライアルを回す | 同じハーネスでエージェントを新たに動かす | 段階2に加えて、Claude Code、herdr、（実験 11 のみ）Quint |

元の実行環境は、Apple silicon の macOS、Go 1.27.4（`/opt/homebrew/bin/go`。fixture の宣言は `go 1.26`）、Claude Code 2.1.332、herdr 0.9.5、Quint 0.33.1。

ハーネスはモデルをエイリアス（`--model haiku` / `--model sonnet`）で渡しており、エイリアスが指すモデルは後で新しいものに変わりうる。元の実行（2026-11-21〜2026-11-26）では、それぞれ `claude-haiku-5-5` と `claude-sonnet-5-5` だった。同じモデルで回すには、`MODEL=claude-haiku-5-5` のように `MODEL` に完全な ID を指定する。

## 1. 表を計算し直す

`scripts/summarize-verdicts.py` は、判定ファイルの組ごとに、タスク別の authz_ok / func_ok / 拒否しすぎを出す。どの判定を並べるかは、`results/*-manifest.tsv` と各レポートを見て選ぶ。TOTAL の行は中心の7タスクの合計である（凍結と、`replication-verdicts` に入っている単発のタスクは含めない）。

```sh
# 実験 11、if 文版 × Haiku: 最初の比較、Quint A、Quint B
scripts/summarize-verdicts.py orig=results/haiku-v2-sql-verdicts \
  A=results/v2-sql-quinta-haiku-verdicts B=results/v2-sql-quintb-haiku-verdicts

# 実験 03: replication-verdicts には両方の構成が入っている。:VARIANT で選ぶ
scripts/summarize-verdicts.py if=results/replication-verdicts:if cap=results/replication-verdicts:cap
```

`verdicts.jsonl` の各行には、落ちた AUTHZ / ATOM / CONTRACT / VALID のサブテスト名も入っている。採点器の生の出力は `results/structure/trials/<grader>-trial<id>.json` で、これを判定に変換するのが `verdict.py`（`.claude/skills/haiku-attempt/scripts/`）である。

## 2. アーカイブ済みのトライアルを採点し直す

提出物は、ベースからの差分として保存してある。ベースは fixture をそのままコミットしたもの。`results/trial-bases.tsv` には、各トライアルの fixture と、このリポジトリで内容を検証できる Git tree SHA を記録している。

`scripts/regrade-trial.sh` は、1本のトライアルについて次をまとめて行う。

1. fixture をコピーする。
2. `results/structure/trials/<id>.diff` を当てる。
3. 凍結済みの採点器を `sandbox-exec` の中で走らせる。
4. 判定を出す。

**最初に1回だけ: `$HOME` の外に Go のモジュールキャッシュを作る。** 採点器は `$HOME` を読めないサンドボックスの中で、`GOPROXY=off` で Go を動かす。

- tanukirpc を使う fixture（`taskboard-cap*`、`-agg`、`-rel`、`-rel-v1`）は vendor 済みなので、準備は要らない。
- SQLite の fixture（`taskboard-*-sql*`）は、共有キャッシュにモジュールを入れておく必要がある。

```sh
export SHARED_GOMODCACHE=/Users/Shared/yapc-gomodcache
for f in fixture/taskboard-v2-sql fixture/taskboard-rel-sql; do
  (cd $f && GOMODCACHE=$SHARED_GOMODCACHE go mod download)
done
```

- `WARM_GOCACHE` に Go のビルドキャッシュを指定すると、採点のたびにサンドボックスへ複製され、ビルドが速くなる。指定は任意。
- Go が `/opt/homebrew/bin` にない場合は、`GO_BIN_DIR` を指定する。Go は `$HOME` の外に入れておくこと。サンドボックスは `$HOME` を読めないので、mise、asdf、goenv などで `$HOME` の下に入れた Go は動かない。
- 採点器の `run.sh` は `git rev-parse` でリポジトリを探す。ZIP でダウンロードした場合（`.git` がない）は、`YAPC_REPO` にリポジトリのルートを指定する。`regrade-trial.sh` は自分で指定する。

**採点し直す:**

```sh
scripts/regrade-trial.sh 1352            # 採点器と中心のサブテストは manifest から読む
scripts/regrade-trial.sh 106 019         # 採点器を明示してもよい
```

結果はアーカイブ済みの判定と比べる（`python3 .claude/skills/haiku-attempt/scripts/verdict.py results/structure/trials/<grader>-trial<id>.json`）。

このリポジトリを作るときに、トライアル 106、307、366、806、1352 をこの方法で作り直した。判定も、落ちたサブテストも、元の実行と一致した。この5本で、cap、cap-noguide、rel-v1、v2-sql、v2-sql-quint の各ベースを確かめたことになる。

**初期トライアル 001〜015。** `results/structure/trials/` に差分があるのは 005、009〜012、015 だけで、これらは `regrade-trial.sh` で採点し直せる。初期トライアルはすべて、ブランチ `haiku-trials/NNN` にもある。

- 1つ目のコミットはベースライン。
- 2つ目のコミットは、提出物と `.trial/`（タスク、設定、メタデータ、採点結果）。

詳しくは [reports/ja/02-trial-branches.md](reports/ja/02-trial-branches.md) を参照。採点器は `results/NNN/` にある。

**採点器。** 各採点器のディレクトリには、次が入っている。

- `zz_grader_test.go`（ハッシュは `FROZEN.sha256`）
- 期待するサブテストの一覧
- `run.sh`

`run.sh` はリポジトリのルートから `grade.sh` を探す。採点器をリポジトリの外にコピーしたときや、`.git` がないときは、`YAPC_REPO` を指定する。

| manifest での採点器名 | ディレクトリ |
|---|---|
| 001〜015 の `NNN`、`004v2` | `results/NNN/` |
| 016〜034 の `NNN` | `results/structure/graders/NNN/` |
| `NNN-200`（SQLite 版と実運用寄せ版） | `results/structure/graders-200/NNN/` |

`results/structure/equivalence-*/` には同等性の確認を記録してある。どの採点器でも、各 fixture が if 文版のベースラインと同じサブテストで落ちること、つまり HTTP から見た振る舞いが同じことを確かめた。

## 3. 新しいトライアルを回す

ハーネスは `.claude/skills/haiku-attempt/` にある Claude Code のスキルである。

- 最初に `SKILL.md` を読むこと。「Pitfalls」の節に、実行を止めたり汚したりした問題がすべてまとめてある。たとえば、止まる Go の shim、権限ダイアログ、セキュリティプラグイン、サンドボックスからの git 設定の抜け道など。
- `SKILL.md` のレイアウトの節にある `yapc2026/haiku-attempt/` は元のモノレポでのパスで、このリポジトリのルートにあたる。

**必要なもの:** `sandbox-exec` のある macOS、zsh、Go、Python 3、jq、git、ログイン済みの `claude` CLI、herdr。`trial-git.sh` は既定で `/Library/Developer/CommandLineTools` の git を使う。別の git 本体を使うときは `TRIAL_GIT_BIN` を指定する（`/usr/bin/git` のシムは不可）。オペレーターのセッションは herdr の中で動き、トライアルごとにエージェントのペインを1つ動かす。

**ディレクトリ**（既定値。環境変数で変えられる）:
- `PROBE_ROOT=~/src/haiku-probe`: ベースのリポジトリとトライアルのクローン。このリポジトリの外に置くこと。エージェントに採点器やほかのトライアルを見せないためである。
- `CONTROL=~/src/haiku-probe-control`: 採点器、トライアルごとの設定、ログ、採点結果。

**手順:**

1. fixture からベースのリポジトリを作る（コミット1つ、"taskboard baseline"）。
   ```sh
   S=.claude/skills/haiku-attempt/scripts
   FIXTURE=$PWD/fixture/taskboard-v2-sql BASE_REPO=$HOME/src/haiku-probe/base-v2-sql \
     GOMODCACHE=$SHARED_GOMODCACHE $S/setup-base.sh
   ```
   キューファイルが使うベースの名前は次のとおり。

   | ベース | fixture |
   |---|---|
   | `base` | `taskboard` |
   | `base-v2` | `taskboard-v2` |
   | `base-cap` | `taskboard-cap` |
   | `base-cap-noguide` | `taskboard-cap-noguide` |
   | `base-agg` | `taskboard-agg` |
   | `base-rel` | `taskboard-rel-v1` |
   | `base-rel2` | `taskboard-rel` |
   | `base-<name>`（SQLite の各版） | `taskboard-<name>` |

2. `grade-manifest.sh` が探す場所に採点器を置く。
   ```sh
   mkdir -p $HOME/src/haiku-probe-control/grader
   for d in results/structure/graders-200/*; do cp -R $d $HOME/src/haiku-probe-control/grader/$(basename $d)-200; done
   for d in results/structure/graders/* results/0* ; do [ -f $d/zz_grader_test.go ] && cp -R $d $HOME/src/haiku-probe-control/grader/$(basename $d); done
   export YAPC_REPO=$PWD
   ```

3. キューファイルを展開して回す。キューの各行は `<trial-id> <base-repo> <task-file>`。
   ```sh
   sed -e "s|@PROBE_ROOT@|$HOME/src/haiku-probe|" -e "s|@REPO@|$PWD|" \
     results/queues/haiku-quintb.queue > /tmp/my.queue
   # $PROBE_ROOT/trials に同じ ID のディレクトリがあるなら、先にトライアル ID を変えること
   export SHARED_GOMODCACHE WARM_GOCACHE=...       # SQLite の fixture
   export QUINT_BIN=/Users/Shared/quint/bin/quint  # 実験 11 のみ
   MODEL=haiku $S/run-queue.sh /tmp/my.queue 7
   ```
   キューファイルがあるのはトライアル 201 以降。005〜160 は、`results/replication-manifest.tsv` と `results/trial-bases.tsv` からキューを作る。

4. ドライバのログ（`$CONTROL/logs/<id>.log`）をすべて確認する。最後が `DONE` で、`dirty=0` であること。アーカイブ済みの実行の状況は `results/run-status.tsv` にある。生のログは公開していない。エージェントが起動しなかったトライアルでも、手つかずのベースラインのまま「有効」と採点されることがある（レポート 05 のトライアル 375）。確認できたら採点する。
   ```sh
   $S/grade-manifest.sh my-manifest.tsv /tmp/verdicts
   ```
   manifest には自分のトライアル ID を書く。アーカイブ済みのもの（例: `results/v2-sql-quintb-haiku-manifest.tsv`）の列を写し、番号を変えたなら ID を置き換える。

**Quint（実験 11）。** トライアルのサンドボックスは `$HOME` を読めず、ネットワークもない。そのため Quint は `$HOME` の外に置き、Rust の評価器も前もって入れておく。

```sh
Q=/Users/Shared/quint; mkdir -p $Q/bin $Q/home
cp "$(which node)" $Q/bin/node
npm install --prefix $Q @informalsystems/quint@0.33.1
cat > $Q/bin/quint <<'EOF'
#!/bin/sh
export QUINT_HOME=/Users/Shared/quint/home
exec /Users/Shared/quint/bin/node /Users/Shared/quint/node_modules/@informalsystems/quint/dist/src/cli.js "$@"
EOF
chmod +x $Q/bin/quint
# 最初の `quint run` が Rust の評価器を QUINT_HOME にダウンロードする。サンドボックスの外で1回だけ実行しておく
$Q/bin/quint run fixture/quint/taskboard.qnt --invariant=invariants --max-steps=5 --max-samples=10
```

`QUINT_BIN` を指定すると、`run-trial.sh` はそのコマンドだけを許可し、`.claude/skills/haiku-attempt/scripts/env-note-quint.md` をエージェントのシステムプロンプトに足す。

**違ってくるもの。** モデルの版は時間とともに変わり、エージェントの実行は決定的ではない。新しいトライアルで再現できるのは実験の設定であり、出力そのものではない。実際に何が起きたかの記録は、アーカイブ済みの差分と判定である。
