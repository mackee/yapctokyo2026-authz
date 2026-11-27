プロジェクトを「凍結」できるようにしてください。

POST /projects/{pid}/freeze で、そのプロジェクトのメンバー全員のロールを viewer に変更し（manager も含みます）、変更後のメンバー一覧を [{"userId":"...","role":"viewer"}] の形で 200 で返します。凍結後は、誰もそのプロジェクトのタスクを変更できなくなります。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
