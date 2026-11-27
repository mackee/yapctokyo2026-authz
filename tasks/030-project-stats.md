プロジェクトの進捗を確認できるようにしてください。

GET /projects/{pid}/stats で、そのプロジェクトのステータスごとのタスク数と担当者ごとのタスク数を {"byStatus":{"open":2,...},"byAssignee":{"ben":1,...}} の形で 200 で返します。

テストも追加し、go test ./... が通る状態で git commit してください。
