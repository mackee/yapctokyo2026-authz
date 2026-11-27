メンバーを別のプロジェクトに異動できるようにしてください（チーム異動に使います）。

POST /projects/{pid}/members/{uid}/move に {"toProjectId":"ops"} を送ると、そのユーザーをこのプロジェクトのメンバーから外し、同じロールで移動先プロジェクトのメンバーに追加して、{"userId":"...","projectId":"ops","role":"..."} を 200 で返します。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
