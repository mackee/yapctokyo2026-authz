プロジェクトメンバーのロールをまとめて変更できるようにしてください。

PUT /projects/{pid}/members に {"roles":{"ben":"viewer","dan":"contributor"}} を送ると、指定したメンバーのロールを一括で変更し、変更後のメンバー一覧を [{"userId":"...","role":"..."}] の形で 200 で返します。ロールは manager / contributor / viewer のいずれかです。1件でも変更できないものがあれば、何も変更せずにエラーを返してください。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
