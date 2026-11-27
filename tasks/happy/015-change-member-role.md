プロジェクトメンバーのロールを変更できるようにしてください。

PUT /projects/{pid}/members/{uid} に {"role":"viewer"} を送ると、そのユーザーのプロジェクト内のロールを変更し、{"userId":"...","role":"..."} を 200 で返します。ロールは manager / contributor / viewer のいずれかです。

プロジェクトの manager は、そのプロジェクトのメンバーのロールを変更できます（昇格も降格もできます）。

go test ./... が通る状態で git commit してください。
