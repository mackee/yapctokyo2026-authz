タスクを別のプロジェクトに移動できるようにしてください。

POST /projects/{pid}/tasks/{tid}/move に {"toProjectId":"ops"} を送ると、そのタスクを指定したプロジェクトに移動し、移動後のタスクを 200 で返します。タスクの ID・タイトル・ステータス・担当者はそのまま引き継ぎます。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
