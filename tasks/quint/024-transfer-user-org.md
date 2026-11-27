ユーザーを別の組織に移籍させられるようにしてください（グループ会社間の異動に使います）。

POST /orgs/{orgID}/users/{uid}/transfer に {"toOrgId":"globex"} を送ると、そのユーザーの所属組織を変更し、{"id":"...","orgId":"..."} を 200 で返します。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
