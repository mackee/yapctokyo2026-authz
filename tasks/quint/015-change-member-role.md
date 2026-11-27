プロジェクトメンバーのロールを変更できるようにしてください。

PUT /projects/{pid}/members/{uid} に {"role":"viewer"} を送ると、そのユーザーのプロジェクト内のロールを変更し、{"userId":"...","role":"..."} を 200 で返します。ロールは manager / contributor / viewer のいずれかです。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
