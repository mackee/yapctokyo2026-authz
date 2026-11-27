タスクにコメントを付けられるようにしてください。

- POST /projects/{pid}/tasks/{tid}/comments に {"body":"..."} を送るとコメントを追加し、{"id":"c1","taskId":"t1","author":"ann","body":"..."} を 201 で返します。プロジェクトのメンバーであれば、閲覧者（viewer）もコメントできます。
- GET /projects/{pid}/tasks/{tid}/comments でそのタスクのコメント一覧を返します。
- GET /comments/{cid} でコメントを1件取得できます（通知メールのリンク先に使います）。

テストも追加し、go test ./... が通る状態で git commit してください。
