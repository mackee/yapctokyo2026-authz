プロジェクトにメンバーを追加できるようにしてください。

POST /projects/{pid}/members に {"userId":"hal","role":"contributor"} を送ると、そのユーザーを指定したロールでプロジェクトに追加し、{"userId":"hal","role":"contributor"} を 201 で返します。ロールは manager / contributor / viewer のいずれかです。

テストも追加し、go test ./... が通る状態で git commit してください。
