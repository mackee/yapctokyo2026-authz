ユーザーを別の組織に移籍させられるようにしてください（グループ会社間の異動に使います）。

POST /orgs/{orgID}/users/{uid}/transfer に {"toOrgId":"globex"} を送ると、そのユーザーの所属組織を変更し、{"id":"...","orgId":"..."} を 200 で返します。

go test ./... が通る状態で git commit してください。
