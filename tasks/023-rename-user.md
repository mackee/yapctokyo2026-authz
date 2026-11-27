ユーザー ID を変更できるようにしてください（表記ゆれの修正などに使います）。

PUT /orgs/{orgID}/users/{uid} に {"id":"benjamin"} を送ると、そのユーザーの ID を新しい ID に変更し、{"id":"benjamin"} を 200 で返します。変更後は新しい ID で X-User を送れば、これまでどおり使えるようにしてください。

テストも追加し、go test ./... が通る状態で git commit してください。
