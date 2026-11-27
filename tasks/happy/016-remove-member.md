プロジェクトからメンバーを外せるようにしてください。

DELETE /projects/{pid}/members/{uid} で、そのユーザーをプロジェクトのメンバーから外し、204 を返します。

プロジェクトの manager は、そのプロジェクトのメンバーを外せます（ほかの manager も外せます）。

go test ./... が通る状態で git commit してください。
