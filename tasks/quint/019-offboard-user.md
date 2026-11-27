組織からユーザーを外せるようにしてください（退職時の処理です）。

DELETE /orgs/{orgID}/users/{uid} で、そのユーザーを組織のすべてのプロジェクトのメンバーから外し、204 を返します。

実装の前に、spec/taskboard.qnt にこの操作の outcome 関数とアクションを追加して step から選ばれるようにし、quint run spec/taskboard.qnt --invariant=invariants で違反が出ないことを確かめてください。実装はモデルに合わせてください。

テストも追加し、go test ./... が通る状態で git commit してください。
