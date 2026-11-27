プロジェクトからメンバーを外せるようにしてください。

DELETE /projects/{pid}/members/{uid} で、そのユーザーをプロジェクトのメンバーから外し、204 を返します。

テストも追加し、go test ./... が通る状態で git commit してください。
