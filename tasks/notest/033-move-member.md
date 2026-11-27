メンバーを別のプロジェクトに異動できるようにしてください（チーム異動に使います）。

POST /projects/{pid}/members/{uid}/move に {"toProjectId":"ops"} を送ると、そのユーザーをこのプロジェクトのメンバーから外し、同じロールで移動先プロジェクトのメンバーに追加して、{"userId":"...","projectId":"ops","role":"..."} を 200 で返します。

go test ./... が通る状態で git commit してください。
